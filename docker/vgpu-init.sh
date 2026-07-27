#!/bin/sh

# Check if the destination directory is provided as an argument
if [ -z "$1" ]; then
    echo "Usage: $0 <destination_directory>"
    exit 1
fi

# Source directory
SOURCE_DIR="/k8s-vgpu/lib/nvidia"

# Destination directory from the argument. Strip trailing slash so we can
# safely compose paths as "$DEST_DIR/$relative_path" below without producing
# double slashes, and so that the preload content we generate at the end
# matches the actual runtime layout exactly.
DEST_DIR="${1%/}"

if [ -z "$DEST_DIR" ]; then
    DEST_DIR="/"
fi


# Check if the destination directory exists, create it if it doesn't
if [ ! -d "$DEST_DIR" ]; then
    mkdir -p "$DEST_DIR"
fi

# Traverse all files in the source directory
find "$SOURCE_DIR" -type f | while read -r source_file; do
    # Get the relative path of the source file
    relative_path="${source_file#$SOURCE_DIR/}"

    # ld.so.preload is intentionally NOT copied from the image source tree.
    # Its content is a runtime pointer to libvgpu.so and MUST match DEST_DIR,
    # otherwise vGPU isolation silently fails when the runtime dir is
    # customized away from the historical /usr/local/vgpu (e.g. via Helm).
    # We regenerate it from DEST_DIR at the end of this script.
    if [ "$relative_path" = "ld.so.preload" ]; then
        echo "Skipped managed file: $source_file"
        continue
    fi

    # Construct the destination file path
    dest_file="$DEST_DIR/$relative_path"

    # If the destination file doesn't exist, copy the source file
    if [ ! -f "$dest_file" ]; then
        # Create the parent directory of the destination file if it doesn't exist
        mkdir -p "$(dirname "$dest_file")"

        # Copy the file from source to destination
        cp "$source_file" "$dest_file"
        echo "Copied: $source_file -> $dest_file"
    else
        # Compare MD5 values of source and destination files
        source_md5=$(md5sum "$source_file" | cut -d ' ' -f 1)
        dest_md5=$(md5sum "$dest_file" | cut -d ' ' -f 1)

        # If MD5 values are different, copy the file
        if [ "$source_md5" != "$dest_md5" ]; then
            cp "$source_file" "$dest_file"
            echo "Copied: $source_file -> $dest_file"
        else
            echo "Skipped (same MD5): $source_file"
        fi
    fi
done

# Regenerate ld.so.preload so it always points at the actual mounted
# libvgpu.so under DEST_DIR. Treating this file as runtime-generated (rather
# than a static packaged asset) is what keeps the preload path aligned with
# whatever runtime directory the chart passes in.
printf '%s/libvgpu.so\n' "$DEST_DIR" > "$DEST_DIR/ld.so.preload"
echo "Updated: $DEST_DIR/ld.so.preload -> $DEST_DIR/libvgpu.so"
