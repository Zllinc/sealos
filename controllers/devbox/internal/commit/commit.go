package commit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"runtime"
	"strings"
	"syscall"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/core/content"
	"github.com/containerd/containerd/v2/core/diff"
	"github.com/containerd/containerd/v2/core/images"
	"github.com/containerd/containerd/v2/core/leases"
	"github.com/containerd/containerd/v2/core/mount"
	"github.com/containerd/containerd/v2/core/remotes"
	"github.com/containerd/containerd/v2/core/remotes/docker"
	"github.com/containerd/containerd/v2/core/remotes/docker/config"
	"github.com/containerd/containerd/v2/core/snapshots"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/containerd/errdefs"
	"github.com/containerd/nerdctl/v2/pkg/api/types"
	"github.com/containerd/nerdctl/v2/pkg/cmd/container"
	"github.com/containerd/nerdctl/v2/pkg/cmd/image"
	"github.com/containerd/nerdctl/v2/pkg/cmd/login"
	"github.com/containerd/nerdctl/v2/pkg/containerutil"
	ncdefaults "github.com/containerd/nerdctl/v2/pkg/defaults"
	"github.com/containerd/platforms"
	"github.com/labring/sealos/controllers/devbox/api/v1alpha2"
	"github.com/labring/sealos/controllers/devbox/internal/commit/utils"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Committer interface {
	CreateContainer(ctx context.Context, devboxName string, contentID string, baseImage string) (string, error)
	CreateContainerNative(ctx context.Context, devboxName string, contentID string, baseImage string) (string, error)
	Commit(ctx context.Context, devboxName string, contentID string, baseImage string, commitImage string) (string, error)
	Push(ctx context.Context, imageName string) error
	RemoveImage(ctx context.Context, imageName []string, force bool, async bool) error
	RemoveContainer(ctx context.Context, containerName string) error
	InitializeGC(ctx context.Context) error
	GC(ctx context.Context) error
	SetLvRemovable(ctx context.Context, containerID string, contentID string) error
}

type CommitterImpl struct {
	containerdClient *containerd.Client          // containerd client
	conn             *grpc.ClientConn            // gRPC connection
	globalOptions    *types.GlobalCommandOptions // global options
	registryAddr     string
	registryUsername string
	registryPassword string
	// Merge base image layers control
	mergeBaseImageTopLayer bool
	// GC
	gcContainerMap map[string]struct{}
	gcImageMap     map[string]struct{}
	gcInterval     time.Duration
	// Commit options
    compressionType   string  // "gzip", "zstd", "uncompressed"
    imageFormat       string  // "oci", "docker"
}

// NewCommitter new a CommitterImpl with registry configuration
func NewCommitter(registryAddr, registryUsername, registryPassword string, merge bool) (Committer, error) {
	var conn *grpc.ClientConn
	var err error

	// login to registry
	err = login.Login(context.Background(), types.LoginCommandOptions{
		GOptions:      *NewGlobalOptionConfig(),
		ServerAddress: registryAddr,
		Username:      registryUsername,
		Password:      registryPassword,
	}, io.Discard)
	if err != nil {
		return nil, err
	}

	// retry to connect
	for i := 0; i <= DefaultMaxRetries; i++ {
		if i > 0 {
			log.Printf("Retrying connection to containerd (attempt %d/%d)...", i, DefaultMaxRetries)
			time.Sleep(DefaultRetryDelay)
		}

		// create gRPC connection
		conn, err = grpc.NewClient(DefaultContainerdAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err == nil {
			log.Printf("Successfully connected to containerd at %s", DefaultContainerdAddress)
			break
		}

		log.Printf("Failed to connect to containerd (attempt %d/%d): %v", i+1, DefaultMaxRetries+1, err)

		if i == DefaultMaxRetries {
			return nil, fmt.Errorf("failed to connect to containerd after %d attempts: %v", DefaultMaxRetries+1, err)
		}
	}

	// create Containerd client
	containerdClient, err := containerd.NewWithConn(conn, containerd.WithDefaultNamespace(DefaultNamespace))
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to create containerd client: %v", err)
	}

	return &CommitterImpl{
		containerdClient:       containerdClient,
		conn:                   conn,
		globalOptions:          NewGlobalOptionConfig(),
		registryAddr:           registryAddr,
		registryUsername:       registryUsername,
		registryPassword:       registryPassword,
		gcContainerMap:         make(map[string]struct{}),
		gcImageMap:             make(map[string]struct{}),
		gcInterval:             DefaultGcInterval,
		mergeBaseImageTopLayer: merge,
		compressionType:        DefaultCompressionType,
		imageFormat:            DefaultImageFormat,
	}, nil
}

// createContainerNative
func (c *CommitterImpl) CreateContainerNative(
	ctx context.Context,
	devboxName string,
	contentID string,
	baseImage string,
) (string, error) {
	// pull image and unpack to devbox snapshotter
	image, err := c.containerdClient.GetImage(ctx, baseImage)
	if err != nil {
		log.Printf("Image %s not found locally, pulling...", baseImage)

		// create resolver for authentication
		resolver, err := GetResolver(ctx, c.registryUsername, c.registryPassword)
		if err != nil {
			return "", fmt.Errorf("failed to create resolver: %w", err)
		}

		// pull image and unpack to devbox snapshotter
		image, err = c.containerdClient.Pull(ctx, baseImage,
			containerd.WithResolver(resolver),
			containerd.WithPullUnpack,
			containerd.WithPullSnapshotter(DefaultDevboxSnapshotter))
		if err != nil {
			return "", fmt.Errorf("failed to pull image %s: %w", baseImage, err)
		}
		log.Printf("Successfully pulled image: %s", baseImage)
	} else {
		// image exists, check if it is unpacked in devbox snapshotter
		unpacked, err := image.IsUnpacked(ctx, DefaultDevboxSnapshotter)
		if err != nil {
			log.Printf("Warning: failed to check if image is unpacked in devbox snapshotter: %v", err)
		} else if !unpacked {
			log.Printf("Image %s exists but not unpacked in devbox snapshotter, unpacking...", baseImage)
			// unpack image in devbox snapshotter
			if err := image.Unpack(ctx, DefaultDevboxSnapshotter); err != nil {
				return "", fmt.Errorf("failed to unpack image %s in devbox snapshotter: %w", baseImage, err)
			}
			log.Printf("Successfully unpacked image %s in devbox snapshotter", baseImage)
		}
	}

	// prepare container labels
	annotations := map[string]string{
		v1alpha2.AnnotationContentID:    contentID,
		v1alpha2.AnnotationStorageLimit: AnnotationUseLimitValue,
		AnnotationKeyNamespace:          DefaultNamespace,
		AnnotationKeyImageName:          baseImage,
	}

	if c.mergeBaseImageTopLayer {
		annotations[v1alpha2.AnnotationInit] = AnnotationImageFromValue
	}

	// prepare snapshot labels
	snapshotLabels := convertLabels(annotations)

	// generate container name
	containerName := fmt.Sprintf("devbox-%s-container-%d", devboxName, time.Now().UnixMicro())
	log.Printf("Creating container with name: %s", containerName)

	// prepare snapshot labels options
	var snapshotOpts []snapshots.Opt
	if len(snapshotLabels) > 0 {
		// add labels to snapshot options
		snapshotOpts = append(snapshotOpts, snapshots.WithLabels(snapshotLabels))
		log.Printf("Snapshot labels: %v", snapshotLabels)
	}

	// prepare OCI spec options
	var specOpts []oci.SpecOpts

	// 1. Start with default OCI spec
	specOpts = append(specOpts, oci.WithDefaultSpec(),
		oci.WithImageConfig(image),
		oci.WithHostNamespace(specs.CgroupNamespace),
	)

	// 2. Apply image config to OCI spec (includes env, working dir, entrypoint, cmd, user, etc.)
	specOpts = append(specOpts, oci.WithImageConfig(image))
	specOpts = append(specOpts, oci.WithDefaultPathEnv)

	// 3. Set cgroup namespace to host mode (required for devbox storage management)
	specOpts = append(specOpts, oci.WithHostNamespace(specs.CgroupNamespace))

	if runtime.GOOS == "linux" {
		specOpts = append(specOpts, oci.WithDefaultUnixDevices)
		specOpts = append(specOpts, oci.WithMounts([]specs.Mount{
			{
				Type:        "cgroup",
				Source:      "cgroup",
				Destination: "/sys/fs/cgroup",
				Options:     []string{"ro", "nosuid", "noexec", "nodev"},
			},
		}))
	}

	// 4. Ensure annotations are propagated to OCI spec
	// Convert annotations map to OCI annotations format
	ociAnnotations := make(map[string]string)
	for k, v := range annotations {
		ociAnnotations[k] = v
	}
	if len(ociAnnotations) > 0 {
		specOpts = append(specOpts, oci.WithAnnotations(ociAnnotations))
	}

	// prepare container options
	var containerOpts []containerd.NewContainerOpts

	// 1. Set snapshotter (must be before WithNewSnapshot)
	containerOpts = append(containerOpts, containerd.WithSnapshotter(DefaultDevboxSnapshotter))

	// 2. Create new snapshot with labels
	containerOpts = append(containerOpts, containerd.WithNewSnapshot(containerName, image, snapshotOpts...))

	// 3. Associate image with container
	containerOpts = append(containerOpts, containerd.WithImage(image))

	// 4. Set image stop signal (default SIGTERM, like nerdctl does)
	containerOpts = append(containerOpts, containerd.WithImageStopSignal(image, "SIGTERM"))

	// 5. Set container labels (annotations)
	containerOpts = append(containerOpts, containerd.WithAdditionalContainerLabels(annotations))

	// 6. Set runtime
	containerOpts = append(containerOpts, containerd.WithRuntime(DefaultRuntime, nil))

	// 7. Apply OCI spec to container
	containerOpts = append(containerOpts, containerd.WithNewSpec(specOpts...))

	// create container object
	container, err := c.containerdClient.NewContainer(ctx, containerName, containerOpts...)
	if err != nil {
		return "", fmt.Errorf("failed to create container %s: %w", containerName, err)
	}

	log.Printf("Container created successfully: %s (ID: %s)", containerName, container.ID())
	return container.ID(), nil
}

func (c *CommitterImpl) CommitNative(ctx context.Context, devboxName string, contentID string, baseImage string, commitImage string) (string, error) {
	fmt.Println("========>>>> commit devbox", devboxName, contentID, baseImage, commitImage)
	ctx = namespaces.WithNamespace(ctx, DefaultNamespace)

	// create container
	containerID, err := c.CreateContainerNative(ctx, devboxName, contentID, baseImage)
	if err != nil {
		return "", fmt.Errorf("failed to create container: %v", err)
	}

	// get container
	container, err := c.containerdClient.LoadContainer(ctx, containerID)
	if err != nil {
		return "", fmt.Errorf("failed to load container: %v", err)
	}

	// get container info
	info, err := container.Info(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get container info: %v", err)
	}

	// container id
	id := container.ID()

	// get base image config
	baseImgWithoutPlatform, err := c.containerdClient.ImageService().Get(ctx, info.Image)
	if err != nil {
		return "", fmt.Errorf("container %q lacks image: %w", id, err)
	}

	// get base image with platform
	platformStr := platforms.DefaultString()
	ocispecPlatform, err := platforms.Parse(platformStr)
	if err != nil {
		return "", err
	}
	platformMC := platforms.Only(ocispecPlatform)
	baseImg := containerd.NewImageWithPlatform(c.containerdClient, baseImgWithoutPlatform, platformMC)

	baseImgConfig, _, err := utils.ReadImageConfig(ctx, baseImg)
	if err != nil {
		return "", err
	}

	// TODO: check if all content exist

	var (
		differ = c.containerdClient.DiffService()
		snName = info.Snapshotter
		sn     = c.containerdClient.SnapshotService(snName)
	)

	// Don't gc me and clean the dirty data after 1 hour!
	ctx, done, err := c.containerdClient.WithLease(ctx, leases.WithRandomID(), leases.WithExpiration(1*time.Hour))
	if err != nil {
		return "", fmt.Errorf("failed to create lease for commit: %w", err)
	}
	defer done(ctx)

	// Sync filesystem to make sure that all the data writes in container could be persisted to disk.
	syscall.Sync()

	// Step 1: Create diff layer (export container changes)
	diffLayerDesc, diffID, err := c.createDiffLayer(ctx, id, sn, c.containerdClient.ContentStore(), differ)
	if err != nil {
		return "", fmt.Errorf("failed to create diff layer: %w", err)
	}

	// Step 2: Generate new image config
	imageConfig, err := c.generateImageConfig(ctx, container, baseImg, baseImgConfig, diffID)
	if err != nil {
		return "", fmt.Errorf("failed to generate image config: %w", err)
	}

	// Step 3: Apply diff layer to snapshotter (create new snapshot chain)
	rootfsID := calculateChainID(imageConfig.RootFS.DiffIDs).String()
	if err := c.applyDiffLayer(ctx, rootfsID, baseImgConfig, sn, differ, diffLayerDesc); err != nil {
		return "", fmt.Errorf("failed to apply diff layer: %w", err)
	}

	// Step 4: Write image contents (config + manifest) to content store
	manifestDesc, configDigest, err := c.writeImageContents(ctx, snName, baseImg, imageConfig, diffLayerDesc)
	if err != nil {
		return "", fmt.Errorf("failed to write image contents: %w", err)
	}

	// Step 5: Create image object in image service
	img := images.Image{
		Name:      commitImage,
		Target:    manifestDesc,
		CreatedAt: time.Now(),
	}

	imageService := c.containerdClient.ImageService()
	if _, err := imageService.Update(ctx, img); err != nil {
		if !errdefs.IsNotFound(err) {
			return "", fmt.Errorf("failed to update image: %w", err)
		}
		if _, err := imageService.Create(ctx, img); err != nil {
			return "", fmt.Errorf("failed to create image %s: %w", commitImage, err)
		}
	}

	// Step 6: Unpack the image to snapshotter (make it runnable)
	committedImage := containerd.NewImage(c.containerdClient, img)
	if err := committedImage.Unpack(ctx, snName); err != nil {
		return "", fmt.Errorf("failed to unpack image: %w", err)
	}

	fmt.Printf("Successfully committed container %s to image %s (digest: %s)\n", id, commitImage, configDigest)
	return containerID, nil
}

// CreateContainer create container with labels
func (c *CommitterImpl) CreateContainer(ctx context.Context, devboxName string, contentID string, baseImage string) (string, error) {
	fmt.Println("========>>>> create container", devboxName, contentID, baseImage)
	ctx = namespaces.WithNamespace(ctx, DefaultNamespace)

	// check connection status, if connection is bad, try to reconnect
	if err := c.CheckConnection(ctx); err != nil {
		log.Printf("Connection check failed: %v, attempting to reconnect...", err)
		if reconnectErr := c.Reconnect(ctx); reconnectErr != nil {
			return "", fmt.Errorf("failed to reconnect: %v", reconnectErr)
		}
	}

	// create container with labels
	originalAnnotations := map[string]string{
		v1alpha2.AnnotationContentID:    contentID,
		v1alpha2.AnnotationStorageLimit: AnnotationUseLimitValue,
		AnnotationKeyNamespace:          DefaultNamespace,
		AnnotationKeyImageName:          baseImage,
	}

	// Add merge base image layers annotation if enabled
	if c.mergeBaseImageTopLayer {
		originalAnnotations[v1alpha2.AnnotationInit] = AnnotationImageFromValue
	}

	// convert labels to "containerd.io/snapshot/devbox-" format
	convertedLabels := convertLabels(originalAnnotations)
	convertedAnnotations := convertMapToSlice(originalAnnotations)

	// create container options
	createOpt := types.ContainerCreateOptions{
		GOptions:       *c.globalOptions,
		Runtime:        DefaultRuntime, // user devbox runtime
		Name:           fmt.Sprintf("devbox-%s-container-%d", devboxName, time.Now().UnixMicro()),
		Pull:           "missing",
		InRun:          false, // not start container
		Rm:             false,
		LogDriver:      "json-file",
		StopSignal:     "SIGTERM",
		Restart:        "unless-stopped",
		Interactive:    false,  // not interactive, avoid conflict with Detach
		Cgroupns:       "host", // add cgroupns mode
		Detach:         true,   // run in background
		Rootfs:         false,
		Label:          convertedAnnotations,
		SnapshotLabels: convertedLabels,
		ImagePullOpt: types.ImagePullOptions{
			GOptions: *c.globalOptions,
		},
	}

	// create network manager
	networkManager, err := containerutil.NewNetworkingOptionsManager(createOpt.GOptions,
		types.NetworkOptions{
			NetworkSlice: []string{DefaultNetworkMode},
		}, c.containerdClient)
	if err != nil {
		log.Println("failed to create network manager:", err)
		return "", fmt.Errorf("failed to create network manager: %v", err)
	}

	// create container
	container, cleanup, err := container.Create(ctx, c.containerdClient, []string{originalAnnotations[AnnotationKeyImageName]}, networkManager, createOpt)
	if err != nil {
		log.Println("failed to create container:", err)
		return "", fmt.Errorf("failed to create container: %v", err)
	}
	if cleanup != nil {
		defer cleanup()
	}

	log.Printf("container created successfully: %s\n", container.ID())
	return container.ID(), nil
}

// DeleteContainer delete container
func (c *CommitterImpl) DeleteContainer(ctx context.Context, containerName string) error {
	ctx = namespaces.WithNamespace(ctx, DefaultNamespace)
	container, err := c.containerdClient.LoadContainer(ctx, containerName)
	if err != nil {
		return fmt.Errorf("failed to load container: %v", err)
	}

	// try to get and stop task
	task, err := container.Task(ctx, nil)
	if err == nil {
		log.Printf("Stopping task for container: %s", containerName)

		// force kill task
		err = task.Kill(ctx, 9) // SIGKILL
		if err != nil {
			log.Printf("Warning: failed to send SIGKILL: %v", err)
		} else {
			log.Printf("Sent SIGKILL to task")
		}

		// delete task
		log.Printf("Deleting task...")
		_, err = task.Delete(ctx, containerd.WithProcessKill)
		if err != nil {
			log.Printf("Warning: failed to delete task: %v", err)
		} else {
			log.Printf("Task deleted for container: %s", containerName)
		}
	}

	// delete container (include snapshot)
	err = container.Delete(ctx, containerd.WithSnapshotCleanup)
	if err != nil {
		return fmt.Errorf("failed to delete container: %v", err)
	}

	log.Printf("Container deleted: %s successfully", containerName)
	return nil
}

func (c *CommitterImpl) SetLvRemovable(ctx context.Context, containerID string, contentID string) error {
	fmt.Println("========>>>> set lv removable for container", contentID)
	ctx = namespaces.WithNamespace(ctx, DefaultNamespace)

	// check connection status, if connection is bad, try to reconnect
	if err := c.CheckConnection(ctx); err != nil {
		log.Printf("Connection check failed: %v, attempting to reconnect...", err)
		if reconnectErr := c.Reconnect(ctx); reconnectErr != nil {
			return fmt.Errorf("failed to reconnect: %v", reconnectErr)
		}
	}

	_, err := c.containerdClient.SnapshotService(DefaultDevboxSnapshotter).Update(ctx, snapshots.Info{
		Name:   containerID,
		Labels: map[string]string{RemoveContentIDkey: contentID},
	}, "labels."+RemoveContentIDkey)
	if err != nil {
		return err
	}
	return nil
}

// RemoveContainer remove container
func (c *CommitterImpl) RemoveContainer(ctx context.Context, containerID string) error {
	// check containerID is not empty
	if containerID == "" {
		return fmt.Errorf("[RemoveContainer]containerID is empty")
	}

	fmt.Println("========>>>> remove container", containerID)
	ctx = namespaces.WithNamespace(ctx, DefaultNamespace)

	// check connection status, if connection is bad, try to reconnect
	if err := c.CheckConnection(ctx); err != nil {
		log.Printf("Connection check failed: %v, attempting to reconnect...", err)
		if reconnectErr := c.Reconnect(ctx); reconnectErr != nil {
			return fmt.Errorf("failed to reconnect: %v", reconnectErr)
		}
	}

	global := NewGlobalOptionConfig()
	opt := types.ContainerRemoveOptions{
		Stdout:   io.Discard,
		Force:    false,
		Volumes:  false,
		GOptions: *global,
	}
	err := container.Remove(ctx, c.containerdClient, []string{containerID}, opt)
	if err != nil {
		return fmt.Errorf("failed to remove container: %v", err)
	}
	return nil
}

// Commit commit container to image
func (c *CommitterImpl) Commit(ctx context.Context, devboxName string, contentID string, baseImage string, commitImage string) (string, error) {
	fmt.Println("========>>>> commit devbox", devboxName, contentID, baseImage, commitImage)
	ctx = namespaces.WithNamespace(ctx, DefaultNamespace)
	containerID, err := c.CreateContainer(ctx, devboxName, contentID, baseImage)
	if err != nil {
		return "", fmt.Errorf("failed to create container: %v", err)
	}

	// // mark for gc
	// defer c.MarkForGC(containerID, commitImage)

	// create commit options
	global := NewGlobalOptionConfig()
	opt := types.ContainerCommitOptions{
		Stdout:   io.Discard,
		GOptions: *global,
		Pause:    PauseContainerDuringCommit,
		// Remove base image top layer:
		DevboxOptions: types.DevboxOptions{
			RemoveBaseImageTopLayer: c.mergeBaseImageTopLayer,
		},
	}

	// commit container
	err = container.Commit(ctx, c.containerdClient, commitImage, containerID, opt)
	if err != nil {
		return "", fmt.Errorf("failed to commit container: %v", err)
	}

	return containerID, nil
}

// GetContainerAnnotations get container annotations
func (c *CommitterImpl) GetContainerAnnotations(ctx context.Context, containerName string) (map[string]string, error) {
	ctx = namespaces.WithNamespace(ctx, DefaultNamespace)
	container, err := c.containerdClient.LoadContainer(ctx, containerName)
	if err != nil {
		return nil, fmt.Errorf("failed to load container: %v", err)
	}

	// get container labels (annotations)
	labels, err := container.Labels(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get container labels: %v", err)
	}
	return labels, nil
}

// Push pushes an image to a remote repository
func (c *CommitterImpl) Push(ctx context.Context, imageName string) error {
	fmt.Println("========>>>> push image", imageName)
	ctx = namespaces.WithNamespace(ctx, DefaultNamespace)

	// check connection status, if connection is bad, try to reconnect
	if err := c.CheckConnection(ctx); err != nil {
		log.Printf("Connection check failed: %v, attempting to reconnect...", err)
		if reconnectErr := c.Reconnect(ctx); reconnectErr != nil {
			return fmt.Errorf("failed to reconnect: %v", reconnectErr)
		}
	}

	//set resolver
	resolver, err := GetResolver(ctx, c.registryUsername, c.registryPassword)
	if err != nil {
		log.Printf("failed to set resolver, Image: %s, err: %v\n", imageName, err)
		return err
	}

	imageRef, err := c.containerdClient.GetImage(ctx, imageName)
	if err != nil {
		log.Printf("failed to get image: %s, err: %v\n", imageName, err)
		return err
	}

	// push image
	err = c.containerdClient.Push(ctx, imageName, imageRef.Target(),
		containerd.WithResolver(resolver),
	)
	if err != nil {
		log.Printf("failed to push image: %s, err: %v\n", imageName, err)
		return err
	}
	log.Printf("Pushed image success Image: %s\n", imageName)
	return nil
}

// RemoveImage remove image
func (c *CommitterImpl) RemoveImage(ctx context.Context, imageName string, force bool, async bool) error {
	fmt.Println("========>>>> remove image", imageName)
	ctx = namespaces.WithNamespace(ctx, DefaultNamespace)

	// check connection status, if connection is bad, try to reconnect
	if err := c.CheckConnection(ctx); err != nil {
		log.Printf("Connection check failed: %v, attempting to reconnect...", err)
		if reconnectErr := c.Reconnect(ctx); reconnectErr != nil {
			return fmt.Errorf("failed to reconnect: %v", reconnectErr)
		}
	}

	global := NewGlobalOptionConfig()
	opt := types.ImageRemoveOptions{
		Stdout:   io.Discard,
		GOptions: *global,
		Force:    force,
		Async:    async,
	}
	return image.Remove(ctx, c.containerdClient, []string{imageName}, opt)
}

// MarkForGC mark container and image for GC
func (c *CommitterImpl) MarkForGC(containerID string, imageID string) {
	c.gcContainerMap[containerID] = struct{}{}
	c.gcImageMap[imageID] = struct{}{}
}

// GC start periodic GC
func (c *CommitterImpl) GC(ctx context.Context) error {
	ticker := time.NewTicker(c.gcInterval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				log.Printf("Starting periodic GC at: %v", time.Now())
				if err := c.normalGC(ctx); err != nil {
					log.Printf("Failed to GC, err: %v", err)
				}
			}
		}
	}()
	return nil
}

// normalGC gc container and image
func (c *CommitterImpl) normalGC(ctx context.Context) error {
	log.Printf("Starting normal GC in namespace: %s", DefaultNamespace)
	ctx = namespaces.WithNamespace(ctx, DefaultNamespace)
	// get all container in namespace
	containers, err := c.containerdClient.Containers(ctx)
	if err != nil {
		log.Printf("Failed to get containers, err: %v", err)
		return err
	}

	// gc container
	for _, container := range containers {
		if _, ok := c.gcContainerMap[container.ID()]; ok {
			err = c.RemoveContainer(ctx, container.ID())
			if err != nil {
				log.Printf("Failed to remove container %s, err: %v", container.ID(), err)
			}
		}
	}

	// clear gcContainerMap
	c.gcContainerMap = make(map[string]struct{})

	// get all image in namespace
	images, err := c.containerdClient.ListImages(ctx)
	if err != nil {
		log.Printf("Failed to get images, err: %v", err)
		return err
	}

	// gc image
	for _, image := range images {
		if _, ok := c.gcImageMap[image.Name()]; ok {
			err = c.RemoveImage(ctx, image.Name(), false, false)
			if err != nil {
				log.Printf("Failed to remove image %s, err: %v", image.Name(), err)
			}
		}
	}

	// clear gcImageMap
	c.gcImageMap = make(map[string]struct{})

	return nil
}

// InitializeGC initialize force GC
func (c *CommitterImpl) InitializeGC(ctx context.Context) error {
	gcCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := c.forceGC(gcCtx); err != nil {
		log.Printf("Failed to initialize force GC, err: %v", err)
		return fmt.Errorf("failed to initialize force GC: %v", err)
	}
	log.Println("Force GC initialized successfully")
	return nil
}

// forceGC force gc container and image
func (c *CommitterImpl) forceGC(ctx context.Context) error {
	log.Printf("Starting force GC in namespace: %s", DefaultNamespace)
	ctx = namespaces.WithNamespace(ctx, DefaultNamespace)
	containers, err := c.containerdClient.Containers(ctx)
	if err != nil {
		log.Printf("Failed to get containers, err: %v", err)
		return err
	}

	// gc container
	for _, container := range containers {
		if err := c.RemoveContainer(ctx, container.ID()); err != nil {
			log.Printf("Failed to remove container %s, err: %v", container.ID(), err)
		}
	}
	c.gcContainerMap = make(map[string]struct{})

	// gc image
	images, err := c.containerdClient.ListImages(ctx)
	if err != nil {
		log.Printf("Failed to get images, err: %v", err)
		return err
	}
	for _, image := range images {
		if err := c.RemoveImage(ctx, image.Name(), false, false); err != nil {
			log.Printf("Failed to remove image %s, err: %v", image.Name(), err)
		}
	}
	c.gcImageMap = make(map[string]struct{})
	return nil
}

// GetResolver get resolver
func GetResolver(ctx context.Context, username string, secret string) (remotes.Resolver, error) {
	resolverOptions := docker.ResolverOptions{
		Tracker: docker.NewInMemoryTracker(),
	}
	hostOptions := config.HostOptions{}
	if username == "" && secret == "" {
		hostOptions.Credentials = nil
	} else {
		// TODO: fix this, use flags or configs to set mulit registry credentials
		hostOptions.Credentials = func(host string) (string, string, error) {
			return username, secret, nil
		}
	}
	hostOptions.DefaultScheme = "http"
	hostOptions.DefaultTLS = nil
	resolverOptions.Hosts = config.ConfigureHosts(ctx, hostOptions)
	return docker.NewResolver(resolverOptions), nil
}

// convertLabels convert labels to "containerd.io/snapshot/devbox-" format
func convertLabels(labels map[string]string) map[string]string {
	convertedLabels := make(map[string]string)
	for key, value := range labels {
		if strings.HasPrefix(key, ContainerLabelPrefix) {
			// convert "devbox.sealos.io/" to "containerd.io/snapshot/devbox-"
			newKey := SnapshotLabelPrefix + key[len(ContainerLabelPrefix):]
			convertedLabels[newKey] = value
		}
	}
	return convertedLabels
}

// convertMapToSlice convert map to slice
func convertMapToSlice(labels map[string]string) []string {
	slice := make([]string, 0, len(labels))
	for key, value := range labels {
		slice = append(slice, fmt.Sprintf("%s=%s", key, value))
	}
	return slice
}

// NewGlobalOptionConfig new global option config
func NewGlobalOptionConfig() *types.GlobalCommandOptions {
	return &types.GlobalCommandOptions{
		Namespace:        DefaultNamespace,
		Address:          DefaultContainerdAddress,
		DataRoot:         DefaultNerdctlDataRoot,
		Debug:            false,
		DebugFull:        false,
		Snapshotter:      DefaultDevboxSnapshotter,
		CNIPath:          ncdefaults.CNIPath(),
		CNINetConfPath:   ncdefaults.CNINetConfPath(),
		CgroupManager:    ncdefaults.CgroupManager(),
		InsecureRegistry: InsecureRegistry,
		HostsDir:         []string{DefaultNerdctlHostsDir},
		Experimental:     true,
		HostGatewayIP:    ncdefaults.HostGatewayIP(),
		KubeHideDupe:     false,
		CDISpecDirs:      ncdefaults.CDISpecDirs(),
		UsernsRemap:      "",
		DNS:              []string{},
		DNSOpts:          []string{},
		DNSSearch:        []string{},
	}
}

// CheckConnection check if the connection is still alive
func (c *CommitterImpl) CheckConnection(ctx context.Context) error {
	if c.conn == nil {
		return fmt.Errorf("connection is nil")
	}

	// check connection state
	state := c.conn.GetState()
	if state.String() == "TRANSIENT_FAILURE" || state.String() == "SHUTDOWN" {
		return fmt.Errorf("connection is in bad state: %s", state.String())
	}

	// try to ping containerd
	_, err := c.containerdClient.Version(ctx)
	if err != nil {
		return fmt.Errorf("failed to ping containerd: %v", err)
	}

	return nil
}

// Reconnect attempt to reconnect to containerd
func (c *CommitterImpl) Reconnect(ctx context.Context) error {
	log.Printf("Attempting to reconnect to containerd...")

	// close old connection
	if c.conn != nil {
		c.conn.Close()
	}

	var conn *grpc.ClientConn
	var err error

	for i := 0; i <= DefaultMaxRetries; i++ {
		if i > 0 {
			log.Printf("Retrying connection to containerd (attempt %d/%d)...", i, DefaultMaxRetries)
			time.Sleep(DefaultRetryDelay)
		}

		// create gRPC connection
		conn, err = grpc.NewClient(DefaultContainerdAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err == nil {
			log.Printf("Successfully connected to containerd at %s", DefaultContainerdAddress)
			break
		}

		log.Printf("Failed to connect to containerd (attempt %d/%d): %v", i+1, DefaultMaxRetries+1, err)

		if i == DefaultMaxRetries {
			return fmt.Errorf("failed to connect to containerd after %d attempts: %v", DefaultMaxRetries+1, err)
		}
	}

	// recreate containerd client
	containerdClient, err := containerd.NewWithConn(conn, containerd.WithDefaultNamespace(DefaultNamespace))
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to recreate containerd client: %v", err)
	}

	// update instance
	c.containerdClient = containerdClient
	c.conn = conn

	log.Printf("Successfully reconnected to containerd")
	return nil
}

// Close close the connection
func (c *CommitterImpl) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// ==================== Commit Native Helper Functions ====================

// createDiffLayer creates a diff layer from container snapshot
func (c *CommitterImpl) createDiffLayer(ctx context.Context, containerID string, sn snapshots.Snapshotter, cs content.Store, differ diff.Comparer) (ocispec.Descriptor, digest.Digest, error) {
	diffOpts := make([]diff.Opt, 0)
    var mediaType string

    switch c.imageFormat {
    case ImageFormatOCI:
        switch c.compressionType {
        case CompressionTypeZstd:
			diffOpts = append(diffOpts, diff.WithMediaType(ocispec.MediaTypeImageLayerZstd))
            mediaType = ocispec.MediaTypeImageLayerZstd
        default: // gzip
		    diffOpts = append(diffOpts, diff.WithMediaType(ocispec.MediaTypeImageLayerGzip))
            mediaType = ocispec.MediaTypeImageLayerGzip
        }
    case ImageFormatDocker:
        switch c.compressionType {
        case CompressionTypeZstd:
			diffOpts = append(diffOpts, diff.WithMediaType(ocispec.MediaTypeImageLayerZstd))
            mediaType = images.MediaTypeDockerSchema2LayerZstd
        default: // gzip
			diffOpts = append(diffOpts, diff.WithMediaType(ocispec.MediaTypeImageLayerGzip))
            mediaType = images.MediaTypeDockerSchema2LayerGzip
        }
    default:
        // Default to Docker Schema2 media types for compatibility
		switch c.compressionType {
		case CompressionTypeZstd:
			diffOpts = append(diffOpts, diff.WithMediaType(ocispec.MediaTypeImageLayerZstd))
			mediaType = images.MediaTypeDockerSchema2LayerZstd
		default:
			diffOpts = append(diffOpts, diff.WithMediaType(ocispec.MediaTypeImageLayerGzip))
			mediaType = images.MediaTypeDockerSchema2LayerGzip
		}
    }
	
	// Get snapshot info
	info, err := sn.Stat(ctx, containerID)
	if err != nil {
		return ocispec.Descriptor{}, "", fmt.Errorf("failed to stat snapshot: %w", err)
	}

	parent := info.Parent
	if c.mergeBaseImageTopLayer {
		secondInfo, err := sn.Stat(ctx, parent)
		if err != nil {
			return ocispec.Descriptor{}, "", fmt.Errorf("failed to stat parent snapshot: %w", err)
		}
		if secondInfo.Parent != "" {
			parent = secondInfo.Parent
		}
	}

	lowerKey := fmt.Sprintf("%s-parent-view-%s", parent, utils.UniquePart())
	lower, err := sn.View(ctx, lowerKey, parent)
	if err != nil {
		return ocispec.Descriptor{}, "", fmt.Errorf("failed to create lower snapshot: %w", err)
	}
	defer doWithTimeout(ctx, func(cleanupCtx context.Context) {
		if err := sn.Remove(cleanupCtx, lowerKey); err != nil {
			log.Printf("Warning: failed to cleanup snapshot %s: %v", lowerKey, err)
		}
	})

	var upper []mount.Mount
	if info.Kind == snapshots.KindActive {
		upper, err = sn.Mounts(ctx, containerID)
		if err != nil {
			return ocispec.Descriptor{}, "", fmt.Errorf("failed to get container mounts: %w", err)
		}
	} else {
		upperKey := fmt.Sprintf("%s-view-%s", containerID, utils.UniquePart())
		upper, err = sn.View(ctx, upperKey, containerID)
		if err != nil {
			return ocispec.Descriptor{}, "", fmt.Errorf("failed to create upper snapshot: %w", err)
		} 
		defer doWithTimeout(ctx, func(cleanupCtx context.Context) {
			if err := sn.Remove(cleanupCtx, upperKey); err != nil {
				log.Printf("Warning: failed to cleanup snapshot %s: %v", upperKey, err)
			}
		})
	}

	desc, err := differ.Compare(ctx, lower, upper, diffOpts...)
	if err != nil {
		return ocispec.Descriptor{}, "", fmt.Errorf("failed to create diff: %w", err)
	}

	// Get the uncompressed digest (diffID)
	csInfo, err := cs.Info(ctx, desc.Digest)
	if err != nil {
		return ocispec.Descriptor{}, "", fmt.Errorf("failed to get diff info: %w", err)
	}

	diffIDStr, ok := csInfo.Labels["containerd.io/uncompressed"]
	if !ok {
		return ocispec.Descriptor{}, "", fmt.Errorf("diff layer missing uncompressed digest")
	}

	diffID, err := digest.Parse(diffIDStr)
	if err != nil {
		return ocispec.Descriptor{}, "", fmt.Errorf("failed to parse diffID: %w", err)
	}

	return ocispec.Descriptor{
		MediaType: mediaType,
		Digest:    desc.Digest,
		Size:      csInfo.Size,
	}, diffID, nil
}

// generateImageConfig generates OCI image config for the committed image
func (c *CommitterImpl) generateImageConfig(ctx context.Context, container containerd.Container, baseImg containerd.Image, baseConfig ocispec.Image, diffID digest.Digest) (ocispec.Image, error) {
	spec, err := container.Spec(ctx)
	if err != nil {
		return ocispec.Image{}, fmt.Errorf("failed to get container spec: %w", err)
	}

	// Copy base config
	newConfig := baseConfig

	// Build created by string from container process
	createdBy := ""
	if spec.Process != nil && len(spec.Process.Args) > 0 {
		createdBy = strings.Join(spec.Process.Args, " ")
	}

	createdTime := time.Now()

	// Remove base image top layer if configured
	if c.mergeBaseImageTopLayer && len(baseConfig.RootFS.DiffIDs) > 1 {
		newConfig.RootFS.DiffIDs = baseConfig.RootFS.DiffIDs[:len(baseConfig.RootFS.DiffIDs)-1]
		newConfig.History = baseConfig.History[:len(baseConfig.History)-1]
	}

	// Append new diff layer
	newConfig.RootFS.DiffIDs = append(newConfig.RootFS.DiffIDs, diffID)

	// Append history entry
	newConfig.History = append(newConfig.History, ocispec.History{
		Created:   &createdTime,
		CreatedBy: createdBy,
		Comment:   fmt.Sprintf("Committed by devbox from container %s", container.ID()),
	})

	newConfig.Created = &createdTime

	return newConfig, nil
}

// applyDiffLayer applies the diff layer to snapshotter, creating a new snapshot chain
func (c *CommitterImpl) applyDiffLayer(ctx context.Context, chainID string, baseConfig ocispec.Image, sn snapshots.Snapshotter, differ diff.Applier, diffDesc ocispec.Descriptor) error {
	// Calculate parent snapshot
	parent := calculateChainID(baseConfig.RootFS.DiffIDs).String()

	// If removing base image top layer, use parent of parent
	if c.mergeBaseImageTopLayer {
		info, err := sn.Stat(ctx, parent)
		if err != nil {
			return fmt.Errorf("failed to stat parent snapshot: %w", err)
		}
		parent = info.Parent
	}

	// Generate temporary snapshot key
	key := fmt.Sprintf("devbox-commit-%d-%s", time.Now().UnixNano(), chainID[:12])

	// Prepare new snapshot based on parent
	mounts, err := sn.Prepare(ctx, key, parent)
	if err != nil {
		return fmt.Errorf("failed to prepare snapshot: %w", err)
	}

	// Cleanup on error
	defer func() {
		if err != nil {
			doWithTimeout(ctx, func(cleanupCtx context.Context) {
				if removeErr := sn.Remove(cleanupCtx, key); removeErr != nil {
					log.Printf("Warning: failed to cleanup snapshot %s: %v", key, removeErr)
				}
			})
		}
	}()

	// Apply diff layer to mounts
	if _, err = differ.Apply(ctx, diffDesc, mounts); err != nil {
		return fmt.Errorf("failed to apply diff: %w", err)
	}

	// Commit as final snapshot with chainID as name
	if err = sn.Commit(ctx, chainID, key); err != nil {
		if !errdefs.IsAlreadyExists(err) {
			return fmt.Errorf("failed to commit snapshot: %w", err)
		}
		// Snapshot already exists, this is fine
	}

	return nil
}

// writeImageContents writes image config and manifest to content store
func (c *CommitterImpl) writeImageContents(ctx context.Context, snapshotterName string, baseImg containerd.Image, newConfig ocispec.Image, diffLayerDesc ocispec.Descriptor) (ocispec.Descriptor, digest.Digest, error) {
	cs := baseImg.ContentStore()

	// 1. Serialize and write image config
	configJSON, err := json.Marshal(newConfig)
	if err != nil {
		return ocispec.Descriptor{}, "", fmt.Errorf("failed to marshal config: %w", err)
	}

	configDesc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageConfig,
		Digest:    digest.FromBytes(configJSON),
		Size:      int64(len(configJSON)),
	}

	// 2. Read base manifest and build new layers list
	baseMfst, err := readManifest(ctx, baseImg)
	if err != nil {
		return ocispec.Descriptor{}, "", fmt.Errorf("failed to read base manifest: %w", err)
	}

	layers := baseMfst.Layers
	if c.mergeBaseImageTopLayer && len(layers) > 1 {
		layers = layers[:len(layers)-1]
	}
	layers = append(layers, diffLayerDesc)

	// 3. Build new manifest
	newMfst := ocispec.Manifest{
		MediaType: ocispec.MediaTypeImageManifest,
		Config:    configDesc,
		Layers:    layers,
	}

	mfstJSON, err := json.Marshal(newMfst)
	if err != nil {
		return ocispec.Descriptor{}, "", fmt.Errorf("failed to marshal manifest: %w", err)
	}

	mfstDesc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageManifest,
		Digest:    digest.FromBytes(mfstJSON),
		Size:      int64(len(mfstJSON)),
	}

	// 4. Write manifest with GC references to all layers
	mfstLabels := map[string]string{
		"containerd.io/gc.ref.content.0": configDesc.Digest.String(),
	}
	for i, layer := range layers {
		mfstLabels[fmt.Sprintf("containerd.io/gc.ref.content.%d", i+1)] = layer.Digest.String()
	}

	if err := content.WriteBlob(ctx, cs, mfstDesc.Digest.String(), bytes.NewReader(mfstJSON), mfstDesc, content.WithLabels(mfstLabels)); err != nil {
		return ocispec.Descriptor{}, "", fmt.Errorf("failed to write manifest: %w", err)
	}

	// 5. Write config with GC reference to snapshotter
	configLabels := map[string]string{
		fmt.Sprintf("containerd.io/gc.ref.snapshot.%s", snapshotterName): calculateChainID(newConfig.RootFS.DiffIDs).String(),
	}

	if err := content.WriteBlob(ctx, cs, configDesc.Digest.String(), bytes.NewReader(configJSON), configDesc, content.WithLabels(configLabels)); err != nil {
		return ocispec.Descriptor{}, "", fmt.Errorf("failed to write config: %w", err)
	}

	return mfstDesc, configDesc.Digest, nil
}

// calculateChainID calculates the ChainID for a list of DiffIDs
// ChainID([]DiffID) = SHA256(ChainID(DiffIDs[:n-1]) + " " + DiffID[n])
func calculateChainID(diffIDs []digest.Digest) digest.Digest {
	if len(diffIDs) == 0 {
		return ""
	}
	if len(diffIDs) == 1 {
		return diffIDs[0]
	}

	// Recursively calculate: ChainID(n) = SHA256(ChainID(n-1) + " " + DiffID(n))
	parent := diffIDs[0]
	for i := 1; i < len(diffIDs); i++ {
		dgst := digest.SHA256.FromString(parent.String() + " " + diffIDs[i].String())
		parent = dgst
	}
	return parent
}

// readManifest reads the OCI manifest from an image
func readManifest(ctx context.Context, img containerd.Image) (ocispec.Manifest, error) {
	var manifest ocispec.Manifest

	// Read manifest from content store
	manifestBlob, err := content.ReadBlob(ctx, img.ContentStore(), img.Target())
	if err != nil {
		return manifest, fmt.Errorf("failed to read manifest blob: %w", err)
	}

	// Unmarshal manifest
	if err := json.Unmarshal(manifestBlob, &manifest); err != nil {
		return manifest, fmt.Errorf("failed to unmarshal manifest: %w", err)
	}

	return manifest, nil
}

// clearCancel wraps a context to ignore parent cancellation while preserving values
type clearCancel struct {
	context.Context
}

func (cc clearCancel) Deadline() (deadline time.Time, ok bool) {
	return // No deadline
}

func (cc clearCancel) Done() <-chan struct{} {
	return nil // Never done
}

func (cc clearCancel) Err() error {
	return nil // No error
}

// doWithTimeout runs the provided function with a context that ignores parent
// cancellation but has a 10 second timeout. This ensures cleanup operations
// complete even if the parent context is cancelled (e.g., user interruption).
func doWithTimeout(ctx context.Context, do func(context.Context)) {
	cleanupCtx, cancel := context.WithTimeout(clearCancel{ctx}, 10*time.Second)
	defer cancel()
	do(cleanupCtx)
}
