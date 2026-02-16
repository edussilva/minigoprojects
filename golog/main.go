package main

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type LogEntry struct {
	Timestamp  time.Time
	IP         string
	User       string
	Method     string
	Path       string
	Status     int
	Bytes      int
	ResponseMs int
	UserAgent  string
}

type LogFormat int

const (
	FormatUnknown LogFormat = iota
	FormatSimple
	FormatApache
	FormatJSON
)

type Stats struct {
	TotalRequests int
	ByMethod      map[string]int
	ByStatus      map[int]int
	ByPath        map[string]int
	ByIP          map[string]int
	ByUserAgent   map[string]int
	AvgResponse   float64
	MaxResponse   int
	TotalBytes    int64
}

func detectFormat(line string) LogFormat {
	if strings.Contains(line, "HTTP/") && strings.Contains(line, "[") {
		return FormatApache
	}

	parts := strings.Fields(line)
	if len(parts) >= 6 {
		// Tenta verificar se é o formato simples
		if _, err := time.Parse("2006-01-02 15:04:05", parts[0]+" "+parts[1]); err == nil {
			return FormatSimple
		}
	}

	if strings.Contains(line, "{") && strings.Contains(line, "}") {
		return FormatJSON
	}
	return FormatUnknown
}

func parseSimpleFormat(line string) (LogEntry, error) {
	parts := strings.Split(line, " ")
	if len(parts) < 7 {
		return LogEntry{}, fmt.Errorf("formato simples inválido")
	}

	timestamp, err := time.Parse("2006-01-02 15:04:05", parts[0]+" "+parts[1])
	if err != nil {
		return LogEntry{}, err
	}

	responseMs := 0
	if len(parts) >= 6 && strings.HasSuffix(parts[5], "ms") {
		msStr := strings.TrimSuffix(parts[5], "ms")
		responseMs, _ = strconv.Atoi(msStr)
	}

	status, _ := strconv.Atoi(parts[4])

	userAgent := ""
	if len(parts) >= 7 {
		userAgent = strings.Trim(strings.Join(parts[6:], " "), "\"")
	}

	return LogEntry{
		Timestamp:  timestamp,
		Method:     parts[2],
		Path:       parts[3],
		Status:     status,
		ResponseMs: responseMs,
		UserAgent:  userAgent,
	}, nil
}

func removeQueryParameters(urlString string) string {
	parts := strings.SplitN(urlString, "?", 2)
	return parts[0]
}

func parseLogLine(line string) (LogEntry, error) {
	parts := strings.Split(line, " ")
	if len(parts) < 7 {
		return LogEntry{}, fmt.Errorf("linha inválida")
	}

	format := detectFormat(line)
	switch format {
	case FormatSimple:
		return parseSimpleFormat(line)
	case FormatApache:
		return parseApacheFormat(line)
	default:
		// Tenta o formato simples como fallback
		if entry, err := parseSimpleFormat(line); err == nil {
			return entry, nil
		}
		return LogEntry{}, fmt.Errorf("formato não suportado")
	}

	// timestamp, _ := time.Parse("2006-01-02 15:04:05", parts[0]+" "+parts[1])

	// responseMs := 0
	// if strings.HasSuffix(parts[5], "ms") {
	// 	msStr := strings.TrimSuffix(parts[5], "ms")
	// 	responseMs, _ = strconv.Atoi(msStr)
	// }

	// status, _ := strconv.Atoi(parts[4])

	// userAgent := strings.Join(parts[6:], " ")
	// userAgent = strings.Trim(userAgent, "\"")
	// path := removeQueryParameters(parts[3])

	// return LogEntry{
	// 	Timestamp:  timestamp,
	// 	Method:     parts[2],
	// 	Path:       path,
	// 	Status:     status,
	// 	ResponseMs: responseMs,
	// 	UserAgent:  userAgent,
	// }, nil
}

func parseApacheFormat(line string) (LogEntry, error) {
	// Exemplo: 127.0.0.1 - john [15/Jan/2024:10:30:45 -0300] "GET /home HTTP/1.1" 200 2326

	ipEnd := strings.Index(line, " ")
	if ipEnd == -1 {
		return LogEntry{}, fmt.Errorf("formato apache inválido")
	}

	ip := line[:ipEnd]
	rest := line[ipEnd+1:]

	// Encontra o usuário (pode ser "-")
	userEnd := strings.Index(rest, " ")
	if userEnd == -1 {
		return LogEntry{}, fmt.Errorf("formato apache inválido")
	}

	user := rest[:userEnd]
	rest = rest[userEnd+1:]

	// Encontra timestamp entre colchetes
	bracketStart := strings.Index(rest, "[")
	bracketEnd := strings.Index(rest, "]")

	if bracketStart == -1 || bracketEnd == -1 {
		return LogEntry{}, fmt.Errorf("formato apache inválido")
	}

	timestampStr := rest[bracketStart+1 : bracketEnd]
	timestamp, err := time.Parse("02/Jan/2006:15:04:05 -0700", timestampStr)
	if err != nil {
		timestamp, err = time.Parse("02/Jan/2006:15:04:05", timestampStr)
		if err != nil {
			return LogEntry{}, fmt.Errorf("timestamp inválido: %v", err)
		}
	}

	rest = rest[bracketEnd+1:]
	// Encontra a requisição entre aspas

	quoteStart := strings.Index(rest, "\"")
	quoteEnd := strings.Index(rest[quoteStart+1:], "\"")
	if quoteStart == -1 || quoteEnd == -1 {
		return LogEntry{}, fmt.Errorf("formato apache inválido")
	}

	request := rest[quoteStart+1 : quoteStart+1+quoteEnd]
	requestParts := strings.Fields(request)

	if len(requestParts) < 2 {
		return LogEntry{}, fmt.Errorf("request inválido")
	}

	method := requestParts[0]
	path := requestParts[1]

	rest = rest[quoteStart+quoteEnd+2:]

	// Status e bytes
	parts := strings.Fields(rest)
	if len(parts) < 2 {
		return LogEntry{}, fmt.Errorf("formato apache inválido")
	}

	status, _ := strconv.Atoi(parts[0])
	bytes, _ := strconv.Atoi(parts[1])

	return LogEntry{
		IP:        ip,
		User:      user,
		Timestamp: timestamp,
		Method:    method,
		Path:      path,
		Status:    status,
		Bytes:     bytes,
	}, nil
}

func proccessLog(filename string, results chan<- Stats, wg *sync.WaitGroup) {
	defer wg.Done()

	file, err := os.Open(filename)
	if err != nil {
		fmt.Printf("Erro ao abrir o arquivo %v\n", err)
		return
	}

	defer file.Close()

	stats := Stats{
		ByMethod:    make(map[string]int),
		ByStatus:    make(map[int]int),
		ByPath:      make(map[string]int),
		ByIP:        make(map[string]int),
		ByUserAgent: make(map[string]int),
	}

	scanner := bufio.NewScanner(file)
	lineNun := 0
	totalResponse := 0

	for scanner.Scan() {
		lineNun++
		entry, err := parseLogLine(scanner.Text())
		if err != nil {
			continue
		}

		stats.TotalRequests++
		stats.ByMethod[entry.Method]++
		stats.ByStatus[entry.Status]++
		stats.ByPath[entry.Path]++

		if entry.IP != "" {
			stats.ByIP[entry.IP]++
		}

		if entry.UserAgent != "" {
			stats.ByUserAgent[entry.UserAgent]++
		}

		totalResponse += entry.ResponseMs

		stats.TotalBytes += int64(entry.Bytes)

		if entry.ResponseMs > stats.MaxResponse {
			stats.MaxResponse = entry.ResponseMs
		}
	}

	if stats.TotalRequests > 0 {
		stats.AvgResponse = float64(totalResponse) / float64(stats.TotalRequests)
	}

	results <- stats
	fmt.Printf("✅ Processado: %s (%d linhas válidas)\n", filename, stats.TotalRequests)
}

func getStatusDescription(code int) string {
	switch code {
	// 1xx: Informational
	case 100:
		return "Continue"
	case 101:
		return "Switching Protocols"
	case 102:
		return "Processing"
	case 103:
		return "Early Hints"

	// 2xx: Success
	case 200:
		return "OK"
	case 201:
		return "Created"
	case 202:
		return "Accepted"
	case 203:
		return "Non-Authoritative Information"
	case 204:
		return "No Content"
	case 205:
		return "Reset Content"
	case 206:
		return "Partial Content"
	case 207:
		return "Multi-Status"
	case 208:
		return "Already Reported"
	case 226:
		return "IM Used"

	// 3xx: Redirection
	case 300:
		return "Multiple Choices"
	case 301:
		return "Moved Permanently"
	case 302:
		return "Found"
	case 303:
		return "See Other"
	case 304:
		return "Not Modified"
	case 305:
		return "Use Proxy"
	case 307:
		return "Temporary Redirect"
	case 308:
		return "Permanent Redirect"

	// 4xx: Client Error
	case 400:
		return "Bad Request"
	case 401:
		return "Unauthorized"
	case 402:
		return "Payment Required"
	case 403:
		return "Forbidden"
	case 404:
		return "Not Found"
	case 405:
		return "Method Not Allowed"
	case 406:
		return "Not Acceptable"
	case 407:
		return "Proxy Authentication Required"
	case 408:
		return "Request Timeout"
	case 409:
		return "Conflict"
	case 410:
		return "Gone"
	case 411:
		return "Length Required"
	case 412:
		return "Precondition Failed"
	case 413:
		return "Payload Too Large"
	case 414:
		return "URI Too Long"
	case 415:
		return "Unsupported Media Type"
	case 416:
		return "Range Not Satisfiable"
	case 417:
		return "Expectation Failed"
	case 418:
		return "I'm a teapot"
	case 421:
		return "Misdirected Request"
	case 422:
		return "Unprocessable Entity"
	case 423:
		return "Locked"
	case 424:
		return "Failed Dependency"
	case 425:
		return "Too Early"
	case 426:
		return "Upgrade Required"
	case 428:
		return "Precondition Required"
	case 429:
		return "Too Many Requests"
	case 431:
		return "Request Header Fields Too Large"
	case 451:
		return "Unavailable For Legal Reasons"

	// 5xx: Server Error
	case 500:
		return "Internal Server Error"
	case 501:
		return "Not Implemented"
	case 502:
		return "Bad Gateway"
	case 503:
		return "Service Unavailable"
	case 504:
		return "Gateway Timeout"
	case 505:
		return "HTTP Version Not Supported"
	case 506:
		return "Variant Also Negotiates"
	case 507:
		return "Insufficient Storage"
	case 508:
		return "Loop Detected"
	case 510:
		return "Not Extended"
	case 511:
		return "Network Authentication Required"

	default:
		return ""
	}
}

func printReport(stats Stats) {
	fmt.Println(strings.Repeat("=", 50))
	fmt.Println("RELATÓRIO DE ANÁLISE DE LOGS")
	fmt.Println(strings.Repeat("=", 50))

	fmt.Printf("\n📊 TOTAL DE REQUISIÇÕES: %d\n\n", stats.TotalRequests)
	fmt.Printf("💾 Total de bytes transferidos: %s\n\n", formatBytes(stats.TotalBytes))

	fmt.Println("📈 REQUISIÇÕES POR MÉTODO:")
	for method, count := range stats.ByMethod {
		percentage := float64(count) / float64(stats.TotalRequests) * 100
		fmt.Printf("  %-8s: %3d (%5.1f%%)\n", method, count, percentage)
	}

	fmt.Println("\n🔴 STATUS CODES:")

	var statusCodes []int
	for code := range stats.ByStatus {
		statusCodes = append(statusCodes, code)
	}

	sort.Ints(statusCodes)

	for _, code := range statusCodes {
		count := stats.ByStatus[code]
		percentage := float64(count) / float64(stats.TotalRequests) * 100
		statusDesc := getStatusDescription(code)
		fmt.Printf("  %3d %-20s: %3d (%5.1f%%)\n", code, statusDesc, count, percentage)
	}

	fmt.Println("\n⏱️  TEMPO DE RESPOSTA:")
	fmt.Printf("  Média: %.2f ms\n", stats.AvgResponse)
	fmt.Printf("  Máximo: %d ms\n", stats.MaxResponse)

	// IPs mais ativos (top 5)
	if len(stats.ByIP) > 0 {
		fmt.Println("\n🌐 IPs MAIS ATIVOS:")
		type ipCount struct {
			IP    string
			Count int
		}

		var ips []ipCount
		for ip, count := range stats.ByIP {
			ips = append(ips, ipCount{ip, count})
		}

		sort.Slice(ips, func(i, j int) bool {
			return ips[i].Count > ips[j].Count
		})

		for i := 0; i < len(ips) && i < 5; i++ {
			ip := ips[i]
			percentage := float64(ip.Count) / float64(stats.TotalRequests) * 100
			fmt.Printf("  %-15s: %4d (%5.1f%%)\n", ip.IP, ip.Count, percentage)
		}
	}

	fmt.Println("\n🔗 ENDPOINTS MAIS ACESSADOS:")

	type PathCount struct {
		Path  string
		Count int
	}

	var pathCounts []PathCount
	for path, count := range stats.ByPath {
		pathCounts = append(pathCounts, PathCount{path, count})
	}

	sort.Slice(pathCounts, func(i, j int) bool {
		return pathCounts[i].Count > pathCounts[j].Count
	})

	for i := 0; i < len(pathCounts) && i < 5; i++ {
		pc := pathCounts[i]
		percentage := float64(pc.Count) / float64(stats.TotalRequests) * 100
		fmt.Printf("  %-20s: %3d (%5.1f%%)\n", truncate(pc.Path, 30), pc.Count, percentage)
	}

	fmt.Println(strings.Repeat("=", 50))
}

func handleLogs(files []string) {

	results := make(chan Stats, 2)
	wg := sync.WaitGroup{}

	for _, file := range files {
		if _, err := os.Stat(file); err == nil {
			wg.Add(1)
			go proccessLog(file, results, &wg)
		}
	}

	// Aguarda as goroutines
	go func() {
		wg.Wait()
		close(results)
	}()

	finalStats := Stats{
		ByMethod:    make(map[string]int),
		ByStatus:    make(map[int]int),
		ByPath:      make(map[string]int),
		ByIP:        make(map[string]int),
		ByUserAgent: make(map[string]int),
	}

	for stats := range results {

		finalStats.TotalRequests += stats.TotalRequests
		finalStats.AvgResponse += stats.AvgResponse
		finalStats.TotalBytes += stats.TotalBytes

		if stats.MaxResponse > finalStats.MaxResponse {
			finalStats.MaxResponse = stats.MaxResponse
		}

		for method, count := range stats.ByMethod {
			finalStats.ByMethod[method] += count
		}

		for status, count := range stats.ByStatus {
			finalStats.ByStatus[status] += count
		}

		for path, count := range stats.ByPath {
			finalStats.ByPath[path] += count
		}

		for ip, count := range stats.ByIP {
			finalStats.ByIP[ip] += count
		}

		for ua, count := range stats.ByUserAgent {
			finalStats.ByUserAgent[ua] += count
		}

		// Média ponderada do tempo de resposta
		if stats.TotalRequests > 0 && finalStats.TotalRequests > 0 {
			weight := float64(stats.TotalRequests) / float64(finalStats.TotalRequests)
			finalStats.AvgResponse += stats.AvgResponse * weight
		}
	}

	printReport(finalStats)
}

func formatBytes(bytes int64) string {
	units := []string{"B", "KB", "MB", "GB"}
	size := float64(bytes)

	for i := 0; i < len(units); i++ {
		if size < 1024.0 || i == len(units)-1 {
			return fmt.Sprintf("%.2f %s", size, units[i])
		}
		size /= 1024.0
	}
	return fmt.Sprintf("%.0f B", size)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func main() {
	if len(os.Args) < 2 {
		fmt.Printf("Usage: %s <file1> [file2] [file3] ...\n", os.Args[0])
		os.Exit(1)
	}

	filenames := os.Args[1:]
	files := []string{}

	for _, file := range filenames {
		_, err := os.Stat(file)
		if err == nil {
			files = append(files, file)
			fmt.Printf("✓ %s\n", file)
		} else {
			fmt.Printf("✗ %s (file not found)\n", file)
		}
	}
	handleLogs(files)
}
