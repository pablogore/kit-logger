# Scripts Directory

This directory contains utility scripts for the go-kit-logger project.

## Coverage Script

### `coverage-complete-report.sh`

A comprehensive coverage reporting script that generates detailed coverage analysis with threshold validation.

#### Features

- **85% Coverage Threshold**: Validates that the project meets a minimum 85% test coverage requirement
- **Detailed Reports**: Generates multiple report formats:
  - Main coverage report (`coverage_report.txt`)
  - Package-by-package analysis (`coverage_packages.txt`)
  - File-by-file analysis (`coverage_files.txt`)
  - HTML coverage report (`coverage_report.html`)
- **Visual Progress Bars**: Shows coverage progress with colored bars and emojis
- **Exit Code Validation**: Returns exit code 1 if threshold is not met, 0 if passed
- **Excludes Mocks**: Automatically excludes generated mock files from coverage calculation

#### Usage

```bash
# Run with default 85% threshold
./scripts/coverage-complete-report.sh

# Run with custom threshold
COVERAGE_THRESHOLD=90 ./scripts/coverage-complete-report.sh

# Run via Makefile
make coverage-threshold
make ci-threshold
```

#### Output Files

All reports are generated in the `coverage/` directory:

- `coverage_report.txt` - Main summary report
- `coverage_packages.txt` - Detailed package analysis
- `coverage_files.txt` - Detailed file analysis
- `coverage_report.html` - Interactive HTML report
- `coverage.out` - Raw coverage data

#### Threshold Validation

The script validates coverage against a configurable threshold (default: 85%):

- ✅ **PASS**: Coverage ≥ 85% (exit code 0)
- ❌ **FAIL**: Coverage < 85% (exit code 1)

#### Integration with CI/CD

The script is integrated into the Makefile targets:

- `make coverage-threshold` - Run coverage with threshold validation
- `make ci-threshold` - Complete CI pipeline with threshold validation
- `make ci-complete` - Complete CI pipeline with detailed coverage analysis

#### Example Output

```
📊 COVERAGE COMPLETE REPORT - GO-KIT-LOGGER
==========================================

📈 COBERTURA TOTAL:
✅ [█████████████████████████████░]  97.9%

🎉 ¡Cumple threshold de 85%!

📄 ARCHIVOS GENERADOS EN coverage/:
  📊 Reporte principal: coverage/coverage_report.txt
  📦 Reporte por paquetes: coverage/coverage_packages.txt
  📄 Reporte por archivos: coverage/coverage_files.txt
  🌐 Reporte HTML: coverage/coverage_report.html
  📁 Datos de coverage: coverage/coverage.out

💡 RECOMENDACIONES:
  ✅ Threshold cumplido - ¡excelente trabajo!
  📈 Considerar: Apuntar a 90%+ para calidad excelente
```

#### Configuration

The script can be configured via environment variables:

- `COVERAGE_THRESHOLD` - Minimum coverage percentage (default: 85)
- `COVERAGE_DIR` - Output directory (default: coverage)

#### Dependencies

- `go` - Go toolchain
- `bc` - Basic calculator (for floating point math)
- Standard Unix tools (grep, awk, sed, etc.)

#### Notes

- The script continues execution even if some tests fail, as long as coverage data can be collected
- Mock files and test data are automatically excluded from coverage calculation
- The script is compatible with macOS and Linux environments
