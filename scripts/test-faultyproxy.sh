#!/bin/bash

# Comprehensive test suite for faulty proxy package
# This script runs all tests for the faulty proxy in isolation

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
PACKAGE_PATH="$PROJECT_ROOT/pkg/faultyproxy"

echo "🧪 Running Faulty Proxy Test Suite"
echo "=================================="
echo "Package: $PACKAGE_PATH"
echo

# Function to run tests with custom flags
run_tests() {
    local test_type="$1"
    local flags="$2"
    local description="$3"
    
    echo "📋 $description"
    echo "   Command: go test $flags ./pkg/faultyproxy"
    echo
    
    cd "$PROJECT_ROOT"
    if go test $flags ./pkg/faultyproxy; then
        echo "✅ $test_type tests passed"
    else
        echo "❌ $test_type tests failed"
        exit 1
    fi
    echo
}

# Function to run benchmarks
run_benchmarks() {
    echo "🏃 Running Benchmarks"
    echo "   Command: go test -bench=. -benchmem ./pkg/faultyproxy"
    echo
    
    cd "$PROJECT_ROOT"
    if go test -bench=. -benchmem ./pkg/faultyproxy; then
        echo "✅ Benchmarks completed"
    else
        echo "❌ Benchmarks failed"
        exit 1
    fi
    echo
}

# Function to run examples
run_examples() {
    echo "📚 Running Examples"
    echo "   Command: go test -run=Example ./pkg/faultyproxy"
    echo
    
    cd "$PROJECT_ROOT"
    if go test -run=Example ./pkg/faultyproxy; then
        echo "✅ Examples passed"
    else
        echo "❌ Examples failed"
        exit 1
    fi
    echo
}

# Function to run coverage analysis
run_coverage() {
    echo "📊 Running Coverage Analysis"
    echo "   Command: go test -coverprofile=coverage.out ./pkg/faultyproxy"
    echo
    
    cd "$PROJECT_ROOT"
    if go test -coverprofile=coverage.out ./pkg/faultyproxy; then
        go tool cover -html=coverage.out -o coverage.html
        coverage_percent=$(go tool cover -func=coverage.out | grep total | awk '{print $3}')
        echo "✅ Coverage analysis completed: $coverage_percent"
        echo "   HTML report: coverage.html"
        rm -f coverage.out
    else
        echo "❌ Coverage analysis failed"
        exit 1
    fi
    echo
}

# Function to run race detection
run_race_tests() {
    echo "🏁 Running Race Detection Tests"
    echo "   Command: go test -race ./pkg/faultyproxy"
    echo
    
    cd "$PROJECT_ROOT"
    if go test -race ./pkg/faultyproxy; then
        echo "✅ Race detection tests passed"
    else
        echo "❌ Race detection tests failed"
        exit 1
    fi
    echo
}

# Parse command line arguments
case "${1:-all}" in
    "unit")
        run_tests "Unit" "-v -run=TestFaultyProxy_ -short" "Unit Tests (fast)"
        ;;
    "integration")
        run_tests "Integration" "-v -run=TestFaultyProxy_Integration" "Integration Tests"
        ;;
    "benchmarks")
        run_benchmarks
        ;;
    "examples")
        run_examples
        ;;
    "coverage")
        run_coverage
        ;;
    "race")
        run_race_tests
        ;;
    "all")
        echo "🚀 Running complete test suite..."
        echo
        
        # Unit tests (fast)
        run_tests "Unit" "-v -run=TestFaultyProxy_ -short" "Unit Tests (excluding load tests)"
        
        # Integration tests
        run_tests "Integration" "-v -run=TestFaultyProxy_Integration" "Integration Tests"
        
        # Examples
        run_examples
        
        # Race detection
        run_race_tests
        
        # Coverage analysis
        run_coverage
        
        # Benchmarks (run last as they take time)
        echo "⚡ Running benchmarks (this may take a while)..."
        run_benchmarks
        
        echo "🎉 All tests completed successfully!"
        ;;
    "help"|"-h"|"--help")
        echo "Usage: $0 [test-type]"
        echo
        echo "Test types:"
        echo "  unit        - Run unit tests only (fast)"
        echo "  integration - Run integration tests"
        echo "  benchmarks  - Run performance benchmarks"
        echo "  examples    - Run example tests"
        echo "  coverage    - Run coverage analysis"
        echo "  race        - Run race detection tests"
        echo "  all         - Run all tests (default)"
        echo "  help        - Show this help message"
        echo
        echo "Examples:"
        echo "  $0 unit"
        echo "  $0 benchmarks"
        echo "  $0 coverage"
        exit 0
        ;;
    *)
        echo "❌ Unknown test type: $1"
        echo "Run '$0 help' for usage information"
        exit 1
        ;;
esac