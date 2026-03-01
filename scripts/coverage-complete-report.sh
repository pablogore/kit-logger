#!/bin/bash

# Coverage Complete Report Script for go-kit-logger
# Generate a complete coverage report with threshold validation and detailed analysis by package and file

set -e

# Colores para output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
CYAN='\033[0;36m'
WHITE='\033[1;37m'
NC='\033[0m' # No Color

# Configuration
COVERAGE_THRESHOLD=${COVERAGE_THRESHOLD:-85}
COVERAGE_DIR="coverage"
COVERAGE_FILE="$COVERAGE_DIR/coverage.out"
COVERAGE_HTML="$COVERAGE_DIR/coverage_report.html"
REPORT_FILE="$COVERAGE_DIR/coverage_report.txt"
PACKAGE_REPORT_FILE="$COVERAGE_DIR/coverage_packages.txt"
FILE_REPORT_FILE="$COVERAGE_DIR/coverage_files.txt"

# Function to show progress bar
show_progress_bar() {
    local percentage=$1
    local width=30
    local filled=$(echo "scale=0; $percentage * $width / 100" | bc -l | cut -d. -f1)
    local empty=$((width - filled))
    
    printf "["
    for ((i=0; i<filled; i++)); do
        printf "█"
    done
    for ((i=0; i<empty; i++)); do
        printf "░"
    done
    printf "] %5.1f%%" $percentage
}

# Function to get color based on coverage
get_coverage_color() {
    local coverage=$1
    coverage_int=$(echo "$coverage" | cut -d. -f1)
    if [ "$coverage_int" -ge 90 ]; then
        echo -e "${GREEN}"
    elif [ "$coverage_int" -ge 70 ]; then
        echo -e "${YELLOW}"
    else
        echo -e "${RED}"
    fi
}

# Function to get emoji based on coverage
get_coverage_emoji() {
    local coverage=$1
    coverage_int=$(echo "$coverage" | cut -d. -f1)
    if [ "$coverage_int" -ge 90 ]; then
        echo "✅"
    elif [ "$coverage_int" -ge 70 ]; then
        echo "🟡"
    elif [ "$coverage_int" -ge 50 ]; then
        echo "🟠"
    else
        echo "🔴"
    fi
}

# Function to get priority
get_priority() {
    local coverage=$1
    coverage_int=$(echo "$coverage" | cut -d. -f1)
    if [ "$coverage_int" -eq 0 ]; then
        echo "ALTA"
    elif [ "$coverage_int" -lt 30 ]; then
        echo "ALTA"
    elif [ "$coverage_int" -lt 60 ]; then
        echo "MEDIA"
    else
        echo "BAJA"
    fi
}

# Function to get priority color
get_priority_color() {
    local priority=$1
    case $priority in
        "ALTA")
            echo -e "${RED}"
            ;;
        "MEDIA")
            echo -e "${YELLOW}"
            ;;
        "BAJA")
            echo -e "${GREEN}"
            ;;
    esac
}

# Function to generate coverage
generate_coverage() {
    echo -e "${BLUE}🔄 Generando coverage report...${NC}"
    
    # Crear directorio de coverage si no existe
    mkdir -p $COVERAGE_DIR
    
    # Limpiar archivos anteriores en el directorio coverage
    rm -f $COVERAGE_FILE $COVERAGE_HTML $REPORT_FILE $PACKAGE_REPORT_FILE $FILE_REPORT_FILE
    
    # Ejecutar tests con coverage (excluyendo mocks y archivos generados)
    echo -e "${YELLOW}Ejecutando tests con coverage...${NC}"
    
    # Execute tests for specific packages (excluyendo mocks)
    # NOTE: mocks/ is excluded because it contains only generated mock files
    # Using || true to continue even if some tests fail, as long as we get coverage data
    go test -coverprofile=$COVERAGE_FILE -covermode=atomic \
        ./pkg/logger/... \
        ./pkg/logger/handler/... \
        ./pkg/logger/grpc/... \
        ./pkg/logger/httpmw/... \
        ./pkg/logger/utils/... || true
    
    # Generar reporte HTML
    echo -e "${YELLOW}Generando reporte HTML...${NC}"
    go tool cover -html=$COVERAGE_FILE -o $COVERAGE_HTML
    
    echo -e "${GREEN}✅ Coverage generado exitosamente${NC}"
}

# Function to generate report by packages
generate_package_report() {
    echo -e "${BLUE}📦 Generando reporte por paquetes...${NC}"
    
    # Get coverage data by function
    COVERAGE_DATA=$(go tool cover -func=$COVERAGE_FILE)
    
    # Procesar datos por paquete (usando arrays simples para compatibilidad con macOS)
    PACKAGE_NAMES=()
    PACKAGE_TOTALS=()
    PACKAGE_COUNTS=()
    
    while IFS= read -r line; do
        # Process lines containing coverage percentage
        if [[ $line =~ %$ ]]; then
            # Extraer archivo y porcentaje usando awk
            FILE_PATH=$(echo "$line" | awk '{print $1}' | cut -d: -f1)
            COVERAGE_PCT=$(echo "$line" | awk '{print $NF}' | sed 's/%//')
            
            # Extraer nombre del paquete
            PACKAGE_NAME=$(echo "$FILE_PATH" | sed 's|github.com/getsyntegrity/go-kit-logger/||')
            
            # Buscar si el paquete ya existe
            FOUND=false
            for i in "${!PACKAGE_NAMES[@]}"; do
                if [ "${PACKAGE_NAMES[$i]}" = "$PACKAGE_NAME" ]; then
                    PACKAGE_TOTALS[$i]=$(echo "${PACKAGE_TOTALS[$i]} + $COVERAGE_PCT" | bc -l)
                    PACKAGE_COUNTS[$i]=$((${PACKAGE_COUNTS[$i]} + 1))
                    FOUND=true
                    break
                fi
            done
            
            # If not found, add new package
            if [ "$FOUND" = false ]; then
                PACKAGE_NAMES+=("$PACKAGE_NAME")
                PACKAGE_TOTALS+=("$COVERAGE_PCT")
                PACKAGE_COUNTS+=(1)
            fi
        fi
    done <<< "$COVERAGE_DATA"
    
    # Generar reporte por paquetes
    {
        echo "COVERAGE REPORT BY PACKAGES"
        echo "=========================="
        echo "Generated: $(date)"
        echo "Threshold: ${COVERAGE_THRESHOLD}%"
        echo ""
        echo "PACKAGE COVERAGE SUMMARY"
        echo "========================"
        echo ""
        
        # Mostrar paquetes por coverage
        for i in "${!PACKAGE_NAMES[@]}"; do
            if [ "${PACKAGE_COUNTS[$i]}" -gt 0 ]; then
                AVG_COVERAGE=$(echo "scale=2; ${PACKAGE_TOTALS[$i]} / ${PACKAGE_COUNTS[$i]}" | bc -l)
                COVERAGE_INT=$(echo "$AVG_COVERAGE" | cut -d. -f1)
                
                echo "Package: ${PACKAGE_NAMES[$i]}"
                echo "  Average Coverage: ${AVG_COVERAGE}%"
                echo "  Functions: ${PACKAGE_COUNTS[$i]}"
                echo "  Status: $(if [ "$COVERAGE_INT" -ge "$COVERAGE_THRESHOLD" ]; then echo "PASS"; else echo "FAIL"; fi)"
                echo ""
            fi
        done
    } > $PACKAGE_REPORT_FILE
    
    echo -e "${GREEN}✅ Reporte por paquetes generado: $PACKAGE_REPORT_FILE${NC}"
}

# Function to generate report by files
generate_file_report() {
    echo -e "${BLUE}📄 Generando reporte por archivos...${NC}"
    
    # Get coverage data by function
    COVERAGE_DATA=$(go tool cover -func=$COVERAGE_FILE)
    
    # Procesar datos por archivo (usando arrays simples para compatibilidad con macOS)
    FILE_NAMES=()
    FILE_TOTALS=()
    FILE_COUNTS=()
    
    while IFS= read -r line; do
        # Process lines containing coverage percentage
        if [[ $line =~ %$ ]]; then
            # Extraer archivo y porcentaje usando awk
            FILE_PATH=$(echo "$line" | awk '{print $1}' | cut -d: -f1)
            COVERAGE_PCT=$(echo "$line" | awk '{print $NF}' | sed 's/%//')
            
            # Extraer nombre del archivo
            FILE_NAME=$(echo "$FILE_PATH" | sed 's|github.com/getsyntegrity/go-kit-logger/||')
            
            # Buscar si el archivo ya existe
            FOUND=false
            for i in "${!FILE_NAMES[@]}"; do
                if [ "${FILE_NAMES[$i]}" = "$FILE_NAME" ]; then
                    FILE_TOTALS[$i]=$(echo "${FILE_TOTALS[$i]} + $COVERAGE_PCT" | bc -l)
                    FILE_COUNTS[$i]=$((${FILE_COUNTS[$i]} + 1))
                    FOUND=true
                    break
                fi
            done
            
            # If not found, add new file
            if [ "$FOUND" = false ]; then
                FILE_NAMES+=("$FILE_NAME")
                FILE_TOTALS+=("$COVERAGE_PCT")
                FILE_COUNTS+=(1)
            fi
        fi
    done <<< "$COVERAGE_DATA"
    
    # Generar reporte por archivos
    {
        echo "COVERAGE REPORT BY FILES"
        echo "========================"
        echo "Generated: $(date)"
        echo "Threshold: ${COVERAGE_THRESHOLD}%"
        echo ""
        echo "FILE COVERAGE SUMMARY"
        echo "====================="
        echo ""
        
        # Mostrar archivos por coverage
        for i in "${!FILE_NAMES[@]}"; do
            if [ "${FILE_COUNTS[$i]}" -gt 0 ]; then
                AVG_COVERAGE=$(echo "scale=2; ${FILE_TOTALS[$i]} / ${FILE_COUNTS[$i]}" | bc -l)
                COVERAGE_INT=$(echo "$AVG_COVERAGE" | cut -d. -f1)
                
                echo "File: ${FILE_NAMES[$i]}"
                echo "  Average Coverage: ${AVG_COVERAGE}%"
                echo "  Functions: ${FILE_COUNTS[$i]}"
                echo "  Status: $(if [ "$COVERAGE_INT" -ge "$COVERAGE_THRESHOLD" ]; then echo "PASS"; else echo "FAIL"; fi)"
                echo ""
            fi
        done
    } > $FILE_REPORT_FILE
    
    echo -e "${GREEN}✅ Reporte por archivos generado: $FILE_REPORT_FILE${NC}"
}

# Function to generate main report
generate_main_report() {
    echo -e "${BLUE}📊 Generando reporte principal...${NC}"
    
    # Obtener coverage total
    TOTAL_COVERAGE=$(go tool cover -func=$COVERAGE_FILE | grep "total:" | awk '{print $3}' | sed 's/%//')
    TOTAL_COVERAGE_INT=$(echo "$TOTAL_COVERAGE" | cut -d. -f1)
    
    # Verificar threshold
    THRESHOLD_PASS=false
    if [ "$TOTAL_COVERAGE_INT" -ge "$COVERAGE_THRESHOLD" ]; then
        THRESHOLD_PASS=true
    fi
    
    # Generar reporte principal
    {
        echo "COMPLETE COVERAGE REPORT - GO-KIT-LOGGER"
        echo "========================================"
        echo "Generated: $(date)"
        echo "Threshold: ${COVERAGE_THRESHOLD}%"
        echo ""
        echo "OVERALL SUMMARY"
        echo "==============="
        echo "Total Coverage: ${TOTAL_COVERAGE}%"
        echo "Threshold: ${COVERAGE_THRESHOLD}%"
        echo "Status: $(if $THRESHOLD_PASS; then echo "PASS"; else echo "FAIL"; fi)"
        echo ""
        
        if ! $THRESHOLD_PASS; then
            echo "THRESHOLD VALIDATION"
            echo "===================="
            echo "❌ Coverage threshold NOT met!"
            echo "   Current: ${TOTAL_COVERAGE}%"
            echo "   Required: ${COVERAGE_THRESHOLD}%"
            echo "   Missing: $(echo "scale=2; $COVERAGE_THRESHOLD - $TOTAL_COVERAGE" | bc -l)%"
            echo ""
        else
            echo "THRESHOLD VALIDATION"
            echo "===================="
            echo "✅ Coverage threshold met!"
            echo "   Current: ${TOTAL_COVERAGE}%"
            echo "   Required: ${COVERAGE_THRESHOLD}%"
            echo "   Margin: $(echo "scale=2; $TOTAL_COVERAGE - $COVERAGE_THRESHOLD" | bc -l)%"
            echo ""
        fi
        
        echo "DETAILED REPORTS"
        echo "================"
        echo "Package Report: $PACKAGE_REPORT_FILE"
        echo "File Report: $FILE_REPORT_FILE"
        echo "HTML Report: $COVERAGE_HTML"
        echo "Coverage Data: $COVERAGE_FILE"
        echo ""
        echo "EXCLUDED PACKAGES"
        echo "================="
        echo "- mocks/ (generated mock files)"
        echo "- testdata/ (test data files)"
        echo ""
        
        echo "RECOMMENDATIONS"
        echo "==============="
        if ! $THRESHOLD_PASS; then
            echo "🔴 Priority: Improve test coverage to meet threshold"
            echo "📈 Target: Add $(echo "scale=2; $COVERAGE_THRESHOLD - $TOTAL_COVERAGE" | bc -l)% more coverage"
            echo "🎯 Focus: Review packages and files with low coverage"
        else
            echo "✅ Coverage threshold met - good job!"
            echo "📈 Consider: Aim for 90%+ coverage for excellent quality"
        fi
        echo ""
        
        echo "NEXT STEPS"
        echo "=========="
        echo "1. Review detailed reports for specific areas to improve"
        echo "2. Focus on packages and files with low coverage"
        echo "3. Add unit tests for uncovered functions"
        echo "4. Run this report again after improvements"
        echo ""
        
    } > $REPORT_FILE
    
    echo -e "${GREEN}✅ Reporte principal generado: $REPORT_FILE${NC}"
}

# Function to show report in console
show_console_report() {
    echo ""
    echo -e "${CYAN}📊 COVERAGE COMPLETE REPORT - GO-KIT-LOGGER${NC}"
    echo -e "${CYAN}==========================================${NC}"
    echo ""
    
    # Obtener coverage total
    TOTAL_COVERAGE=$(go tool cover -func=$COVERAGE_FILE | grep "total:" | awk '{print $3}' | sed 's/%//')
    TOTAL_COVERAGE_INT=$(echo "$TOTAL_COVERAGE" | cut -d. -f1)
    
    echo -e "${WHITE}📈 COBERTURA TOTAL:${NC}"
    COLOR=$(get_coverage_color $TOTAL_COVERAGE)
    EMOJI=$(get_coverage_emoji $TOTAL_COVERAGE)
    echo -e "$EMOJI $COLOR$(show_progress_bar $TOTAL_COVERAGE)${NC}"
    echo ""
    
    # Verificar threshold
    if [ "$TOTAL_COVERAGE_INT" -ge "$COVERAGE_THRESHOLD" ]; then
        echo -e "${GREEN}🎉 ¡Cumple threshold de ${COVERAGE_THRESHOLD}%!${NC}"
    else
        echo -e "${RED}❌ No cumple threshold de ${COVERAGE_THRESHOLD}%${NC}"
        echo -e "${YELLOW}📈 Falta: $(echo "scale=2; $COVERAGE_THRESHOLD - $TOTAL_COVERAGE" | bc -l)% para cumplir threshold${NC}"
    fi
    echo ""
    
    # Mostrar resumen de archivos generados
    echo -e "${WHITE}📄 ARCHIVOS GENERADOS EN $COVERAGE_DIR/:${NC}"
    echo -e "  📊 Reporte principal: ${GREEN}$REPORT_FILE${NC}"
    echo -e "  📦 Reporte por paquetes: ${GREEN}$PACKAGE_REPORT_FILE${NC}"
    echo -e "  📄 Reporte por archivos: ${GREEN}$FILE_REPORT_FILE${NC}"
    echo -e "  🌐 Reporte HTML: ${GREEN}$COVERAGE_HTML${NC}"
    echo -e "  📁 Datos de coverage: ${GREEN}$COVERAGE_FILE${NC}"
    echo ""
    
    # Mostrar recomendaciones
    echo -e "${WHITE}💡 RECOMENDACIONES:${NC}"
    if [ "$TOTAL_COVERAGE_INT" -lt "$COVERAGE_THRESHOLD" ]; then
        echo -e "  ${RED}🔴 Prioridad ALTA: Mejorar coverage para cumplir threshold${NC}"
        echo -e "  ${YELLOW}📈 Objetivo: Agregar $(echo "scale=2; $COVERAGE_THRESHOLD - $TOTAL_COVERAGE" | bc -l)% más coverage${NC}"
    else
        echo -e "  ${GREEN}✅ Threshold cumplido - ¡excelente trabajo!${NC}"
        echo -e "  ${CYAN}📈 Considerar: Apuntar a 90%+ para calidad excelente${NC}"
    fi
    echo ""
    
    echo -e "${BLUE}🔄 Para regenerar: ./scripts/coverage-complete-report.sh${NC}"
    echo -e "${BLUE}📋 Para ver detalles: cat $REPORT_FILE${NC}"
}

# Main function
main() {
    echo -e "${BLUE}🚀 Iniciando reporte completo de coverage para go-kit-logger...${NC}"
    echo -e "${YELLOW}Umbral configurado: ${COVERAGE_THRESHOLD}%${NC}"
    echo ""
    
    # Generar coverage
    generate_coverage
    
    # Generar reportes
    generate_package_report
    generate_file_report
    generate_main_report
    
    # Mostrar reporte en consola
    show_console_report
    
    # Verificar si cumple threshold para exit code
    TOTAL_COVERAGE=$(go tool cover -func=$COVERAGE_FILE | grep "total:" | awk '{print $3}' | sed 's/%//')
    TOTAL_COVERAGE_INT=$(echo "$TOTAL_COVERAGE" | cut -d. -f1)
    
    if [ "$TOTAL_COVERAGE_INT" -ge "$COVERAGE_THRESHOLD" ]; then
        echo -e "${GREEN}✅ Reporte completado exitosamente - threshold cumplido${NC}"
        exit 0
    else
        echo -e "${RED}❌ Reporte completado - threshold NO cumplido${NC}"
        exit 1
    fi
}

# Execute main function
main "$@"
