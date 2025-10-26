#!/bin/bash

# Playwright Permission Tests for swe-swe
# Prerequisites:
# 1. swe-swe server must be running at localhost:7000
# 2. Run: cd tests/playwright && npm install

echo "================================"
echo "swe-swe Permission Dialog Tests"
echo "================================"
echo ""

# Check if we're in the right directory
if [ ! -f "package.json" ]; then
    echo "Please run this script from the tests/playwright directory"
    echo "Usage: cd tests/playwright && ./run-tests.sh"
    exit 1
fi

# Check if node_modules exists
if [ ! -d "node_modules" ]; then
    echo "Installing dependencies..."
    npm install
fi

# Check if server is running
if ! curl -s http://localhost:7000 > /dev/null; then
    echo "❌ Error: swe-swe server is not running at localhost:7000"
    echo "Please start the server first:"
    echo "  cd ../.. && bin/swe-swe -agent claude"
    exit 1
fi

echo "✅ Server is running at localhost:7000"
echo ""

# Run tests
echo "Running permission dialog tests..."
echo "================================"

# Run specific test file with headed mode for visibility
npx playwright test specs/permission-simple.spec.ts --headed --reporter=list

# Check test results
if [ $? -eq 0 ]; then
    echo ""
    echo "================================"
    echo "✅ All tests passed!"
    echo "================================"
else
    echo ""
    echo "================================"
    echo "❌ Some tests failed"
    echo "Check the output above for details"
    echo "================================"
    exit 1
fi