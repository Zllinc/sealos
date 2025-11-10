package commit

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/stretchr/testify/assert"

	"github.com/labring/sealos/controllers/devbox/api/v1alpha2"
)

const (
	testBaseImage = "docker.io/library/alpine:latest"
)

// TestCreateContainerNative test create container native
func TestCreateContainerNative(t *testing.T) {
	ctx := context.Background()
	ctx = namespaces.WithNamespace(ctx, DefaultNamespace)

	// 1. create committer instance
	fmt.Println("create committer instance")
	committer, err := NewCommitter("sealos.hub:5000", "admin", "5e79497edb8bafb9", false)
	assert.NoError(t, err, "should be able to create committer instance")
	assert.NotNil(t, committer, "committer instance should not be nil")

	fmt.Println("committer instance created")

	impl := committer.(*CommitterImpl)
	defer impl.Close()

	// 2. prepare test data
	devboxName := fmt.Sprintf("test-native-%d", time.Now().Unix())
	contentID := fmt.Sprintf("test-content-id-%d", time.Now().Unix())

	fmt.Println("devboxName:", devboxName)
	fmt.Println("contentID:", contentID)
	fmt.Println("testBaseImage:", testBaseImage)

	// 3. call native create container function
	containerID, err := impl.CreateContainerNative(
		ctx,
		devboxName,
		contentID,
		testBaseImage,
	)

	// 4. verify container created successfully
	assert.NoError(t, err, "should be able to create container")
	assert.NotEmpty(t, containerID, "container ID should not be empty")

	fmt.Printf("✓ successfully created container, container ID: %s\n", containerID)

	// 5. verify container can be loaded by ID
	loadedContainer, err := impl.containerdClient.LoadContainer(ctx, containerID)
	assert.NoError(t, err, "should be able to load container by ID")
	assert.Equal(t, containerID, loadedContainer.ID(), "loaded container ID should match")

	// 6. verify container labels
	labels, err := loadedContainer.Labels(ctx)
	assert.NoError(t, err, "should be able to get container labels")
	assert.NotEmpty(t, labels, "container labels should not be empty")
	assert.Equal(t, contentID, labels[v1alpha2.AnnotationContentID], "content ID should match")
	fmt.Printf("✓ container labels verified successfully\n")
}
