#!/bin/bash
# Check HTTP status of all public routes (M5-11)

BASE_URL="http://localhost:8080"

echo "Checking public routes..."
echo ""

# Public routes
routes=(
  "/"
  "/near-misses"
  "/log"
  "/log?kind=routing"
  "/log?kind=all"
  "/log?kind=ingest"
  "/eval"
  "/health"
  "/version"
  "/static/app.css"
  "/static/timezone-local.js"
)

# Check each route
for route in "${routes[@]}"; do
  status=$(curl -s -o /dev/null -w "%{http_code}" "$BASE_URL$route")
  echo "$route: $status"
done

# Check a pair route (need to get a valid ID from the app)
echo ""
echo "Note: /pairs/{id} and /decisions/{id} routes need valid IDs from seeded data"
