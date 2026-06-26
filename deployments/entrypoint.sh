#!/bin/sh
set -e

# Run migrations before starting the server.
# Exit immediately if migration fails (fail-closed).
echo "Running migrations..."
mez-go-mono migrate up || {
    echo "Migration failed. Container will not start."
    exit 1
}

echo "Starting server..."
exec mez-go-mono serve
