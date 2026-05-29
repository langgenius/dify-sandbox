#!/bin/bash

# Configuration
ARCH="${1:-x86_64}"  # Default to x86_64 if no parameter provided
PYTHON_SCRIPT="cmd/test/syscall_dig/test.py"
STRACE_LOG="strace.log"
TEMP_SYSCALL_NAME="/tmp/syscall_names.log"
MAPPING_FILE="syscall_mapping.log"
NUMBER_LIST="syscall_numbers.log"
SYSCALL_DB="syscall_db.log"

echo "=== Starting ==="
echo "Architecture: $ARCH"

# Build complete syscall database from ausyscall
echo "Building syscall database..."
ausyscall x86_64 --dump > "$SYSCALL_DB"
echo "Total syscalls in database: $(wc -l < "$SYSCALL_DB")"

# Run strace
echo "Running strace..."
strace -o "$STRACE_LOG" -c python3 "$PYTHON_SCRIPT"

# Extract syscall names
echo "Extracting syscall names..."
grep -v '^%\|^--\|^$' "$STRACE_LOG" | grep -v 'total' | awk '{print $NF}' > "$TEMP_SYSCALL_NAME"
cat "$TEMP_SYSCALL_NAME"

# Map to numbers
echo "Generating mapping..."
> "$MAPPING_FILE"
while read name; do
    [ -z "$name" ] && continue
    # Exact match from database
    num=$(grep -w "$name$" "$SYSCALL_DB" | awk '{print $1}')
    if [ -n "$num" ]; then
        echo "$name:$num" >> "$MAPPING_FILE"
    else
        echo "$name:NOT_FOUND" >> "$MAPPING_FILE"
    fi
done < "$TEMP_SYSCALL_NAME"

# Generate number list (comma-separated)
echo "Generating number list..."
grep -v "NOT_FOUND" "$MAPPING_FILE" | cut -d':' -f2 | paste -sd ',' | tr -d '\n' > "$NUMBER_LIST"

# Output results
echo ""
echo "=== Mapping Results ==="
cat "$MAPPING_FILE"

echo ""
echo "=== Number List (comma-separated) ==="
cat "$NUMBER_LIST"

echo ""
echo "=== Done ==="
echo "Mapping file: $MAPPING_FILE"
echo "Number list: $NUMBER_LIST"

# Cleanup
rm -f "$TEMP_SYSCALL_NAME"