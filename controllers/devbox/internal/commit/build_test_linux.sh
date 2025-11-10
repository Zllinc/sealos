#!/bin/bash
# Build test binary for Linux and transfer to remote machine

set -e

# ====== configuration area ======
# target platform
export GOOS=linux
export GOARCH=amd64

# output file name
OUTPUT_FILE="commit.test.linux"

# remote machine configuration (if auto transfer is needed)
# format: user@host
REMOTE_HOST="${REMOTE_HOST:-root@your-linux-server}"
# remote target path
REMOTE_PATH="${REMOTE_PATH:-/root/devbox-test/}"

# whether to auto transfer (set to "yes" to enable auto transfer)
AUTO_TRANSFER="${AUTO_TRANSFER:-no}"

# ====== compile stage ======
echo "================================================"
echo "Building test binary for Linux (amd64)..."
echo "Output: $OUTPUT_FILE"
echo "================================================"

# compile test
go test -c -o "$OUTPUT_FILE" -v

if [ $? -ne 0 ]; then
    echo "✗ Build failed!"
    exit 1
fi

echo "✓ Build successful!"
echo ""

# ====== calculate MD5 ======
echo "================================================"
echo "Calculating MD5 checksum..."
echo "================================================"

# macOS uses md5, Linux uses md5sum
if command -v md5sum &> /dev/null; then
    LOCAL_MD5=$(md5sum "$OUTPUT_FILE" | awk '{print $1}')
elif command -v md5 &> /dev/null; then
    LOCAL_MD5=$(md5 -q "$OUTPUT_FILE")
else
    echo "Warning: md5/md5sum not found, skipping checksum"
    LOCAL_MD5="N/A"
fi

echo "  File: $OUTPUT_FILE"
echo "  MD5:  $LOCAL_MD5"
echo ""

# ====== transfer stage ======
if [ "$AUTO_TRANSFER" = "yes" ]; then
    echo "================================================"
    echo "Transferring to remote machine..."
    echo "================================================"
    echo "  Remote: $REMOTE_HOST"
    echo "  Path:   $REMOTE_PATH"
    echo ""
    
    # create remote directory (if not exists)
    ssh "$REMOTE_HOST" "mkdir -p $REMOTE_PATH" 2>/dev/null || true
    
    # transfer file
    if scp "$OUTPUT_FILE" "$REMOTE_HOST:$REMOTE_PATH"; then
        echo "✓ Transfer successful!"
        echo ""
        
        # calculate MD5 on remote machine
        echo "================================================"
        echo "Verifying MD5 on remote machine..."
        echo "================================================"
        REMOTE_MD5=$(ssh "$REMOTE_HOST" "md5sum $REMOTE_PATH/$OUTPUT_FILE 2>/dev/null | awk '{print \$1}'")
        
        echo "  Local MD5:  $LOCAL_MD5"
        echo "  Remote MD5: $REMOTE_MD5"
        
        if [ "$LOCAL_MD5" = "$REMOTE_MD5" ]; then
            echo "✓ MD5 checksum verified!"
        else
            echo "✗ MD5 checksum mismatch!"
            exit 1
        fi
        echo ""
        
        # add execution permission
        ssh "$REMOTE_HOST" "chmod +x $REMOTE_PATH/$OUTPUT_FILE"
        
        echo "================================================"
        echo "To run the test on remote machine:"
        echo "================================================"
        echo "  ssh $REMOTE_HOST"
        echo "  cd $REMOTE_PATH"
        echo "  ./$OUTPUT_FILE -test.v -test.run TestCreateContainerNative"
        echo ""
    else
        echo "✗ Transfer failed!"
        exit 1
    fi
else
    echo "================================================"
    echo "Manual transfer instructions:"
    echo "================================================"
    echo "To transfer to remote machine, run:"
    echo "  scp $OUTPUT_FILE $REMOTE_HOST:$REMOTE_PATH"
    echo ""
    echo "Or enable auto transfer:"
    echo "  export AUTO_TRANSFER=yes"
    echo "  export REMOTE_HOST=root@your-linux-server"
    echo "  export REMOTE_PATH=/root/devbox-test/"
    echo "  ./build_test_linux.sh"
    echo ""
fi

echo "================================================"
echo "Local run instructions (if on Linux):"
echo "================================================"
echo "  ./$OUTPUT_FILE -test.v -test.run TestCreateContainerNative"
echo ""

