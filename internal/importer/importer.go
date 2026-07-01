package importer

import (
	"bufio"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	cfgpkg "GoNetWatch/internal/config"
	"GoNetWatch/internal/models"

	"gopkg.in/yaml.v3"
)

// ImportOptions configures target import behavior.
type ImportOptions struct {
	Input        string
	Output       string
	DefaultType  string
	Interval     int
	Timeout      int
	Retries      int
	RetryDelayMS int
	Append       bool
	DryRun       bool
}

// Run imports monitoring targets from a text or CSV file into a YAML config.
func Run(opts ImportOptions) error {
	if opts.Input == "" {
		return errors.New("--input is required")
	}
	if opts.Output == "" {
		return errors.New("--output is required")
	}
	if !isAllowedDefaultType(opts.DefaultType) {
		return fmt.Errorf("unsupported --default-type %q: allowed values are http, http-head, tcp, dns", opts.DefaultType)
	}

	slog.Info("import-targets",
		slog.String("input", opts.Input),
		slog.String("output", opts.Output),
		slog.String("default_type", opts.DefaultType),
		slog.Int("interval", opts.Interval),
		slog.Int("timeout", opts.Timeout),
		slog.Int("retries", opts.Retries),
		slog.Int("retry_delay_ms", opts.RetryDelayMS),
		slog.Bool("append", opts.Append),
		slog.Bool("dry_run", opts.DryRun),
	)

	cfg, existingData, mode, err := loadOutputConfig(opts.Output)
	if err != nil {
		return err
	}

	imported, err := parseInput(opts)
	if err != nil {
		return err
	}

	existing := make(map[string]struct{}, len(cfg.Targets))
	for _, target := range cfg.Targets {
		existing[dedupeKey(target)] = struct{}{}
	}

	var added []models.Target
	var skipped []models.Target
	for _, target := range imported {
		key := dedupeKey(target)
		if _, ok := existing[key]; ok {
			skipped = append(skipped, target)
			statusKey := normalizeExpectedStatuses(target.ExpectedStatuses)
			slog.Info("skipped duplicate",
				slog.String("type", target.Type),
				slog.String("address", target.Address),
				slog.String("resolver", target.Resolver),
				slog.String("expected_statuses", statusKey),
			)
			continue
		}

		added = append(added, target)
		existing[key] = struct{}{}
	}

	cfg.Targets = append(cfg.Targets, added...)

	if err := cfgpkg.Validate(cfg); err != nil {
		return err
	}

	if opts.DryRun {
		printDryRun(added, skipped, len(cfg.Targets))
		return nil
	}

	backupPath, err := writeBackup(opts.Output, existingData, mode)
	if err != nil {
		return err
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling updated config: %w", err)
	}
	if err := os.WriteFile(opts.Output, data, mode); err != nil {
		return fmt.Errorf("writing updated config: %w", err)
	}

	slog.Info("config updated",
		slog.String("output", opts.Output),
		slog.String("backup", backupPath),
		slog.Int("added_targets", len(added)),
		slog.Int("total_targets", len(cfg.Targets)),
	)

	return nil
}

func loadOutputConfig(path string) (models.Config, []byte, os.FileMode, error) {
	var cfg models.Config

	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, nil, 0, fmt.Errorf("reading output config: %w", err)
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, nil, 0, fmt.Errorf("parsing output config: %w", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		return cfg, nil, 0, fmt.Errorf("stat output config: %w", err)
	}

	return cfg, data, info.Mode().Perm(), nil
}

func parseInput(opts ImportOptions) ([]models.Target, error) {
	if strings.EqualFold(filepath.Ext(opts.Input), ".csv") {
		return parseCSV(opts)
	}
	return parseTXT(opts)
}

func parseTXT(opts ImportOptions) ([]models.Target, error) {
	file, err := os.Open(opts.Input)
	if err != nil {
		return nil, fmt.Errorf("opening input file: %w", err)
	}
	defer file.Close()

	var targets []models.Target
	scanner := bufio.NewScanner(file)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}

		target, err := targetFromLine(raw, opts)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		targets = append(targets, target)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading input file: %w", err)
	}

	return targets, nil
}

func parseCSV(opts ImportOptions) ([]models.Target, error) {
	file, err := os.Open(opts.Input)
	if err != nil {
		return nil, fmt.Errorf("opening input file: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1

	headers, err := reader.Read()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("csv input is empty")
		}
		return nil, fmt.Errorf("reading csv header: %w", err)
	}

	columns, err := parseCSVHeaders(headers)
	if err != nil {
		return nil, err
	}
	if _, ok := columns["address"]; !ok {
		return nil, errors.New("csv header must include address")
	}

	var targets []models.Target
	lineNo := 1
	for {
		lineNo++
		record, err := reader.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("line %d: reading csv record: %w", lineNo, err)
		}
		if isEmptyCSVRecord(record) {
			continue
		}

		target, err := targetFromCSVRecord(record, columns, opts)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		targets = append(targets, target)
	}

	return targets, nil
}

func parseCSVHeaders(headers []string) (map[string]int, error) {
	allowed := map[string]struct{}{
		"name":              {},
		"type":              {},
		"address":           {},
		"interval_sec":      {},
		"timeout_sec":       {},
		"retries":           {},
		"retry_delay_ms":    {},
		"resolver":          {},
		"expected_statuses": {},
	}

	columns := make(map[string]int, len(headers))
	for i, header := range headers {
		name := strings.TrimSpace(header)
		if name == "" {
			return nil, fmt.Errorf("csv header column %d is empty", i+1)
		}
		if _, ok := allowed[name]; !ok {
			return nil, fmt.Errorf("unknown csv column %q", name)
		}
		if _, ok := columns[name]; ok {
			return nil, fmt.Errorf("duplicate csv column %q", name)
		}
		columns[name] = i
	}

	return columns, nil
}

func targetFromCSVRecord(record []string, columns map[string]int, opts ImportOptions) (models.Target, error) {
	address := csvValue(record, columns, "address")
	if address == "" {
		return models.Target{}, errors.New("address is required")
	}

	targetType := csvValue(record, columns, "type")
	if targetType == "" {
		targetType = opts.DefaultType
	}
	if !isAllowedDefaultType(targetType) {
		return models.Target{}, fmt.Errorf("unsupported type %q: allowed values are http, http-head, tcp, dns", targetType)
	}

	interval, err := csvOptionalInt(record, columns, "interval_sec", opts.Interval)
	if err != nil {
		return models.Target{}, err
	}
	timeout, err := csvOptionalInt(record, columns, "timeout_sec", opts.Timeout)
	if err != nil {
		return models.Target{}, err
	}
	retries, err := csvOptionalInt(record, columns, "retries", opts.Retries)
	if err != nil {
		return models.Target{}, err
	}
	retryDelayMS, err := csvOptionalInt(record, columns, "retry_delay_ms", opts.RetryDelayMS)
	if err != nil {
		return models.Target{}, err
	}

	resolver := ""
	if targetType == "dns" {
		resolver = csvValue(record, columns, "resolver")
	}

	name := csvValue(record, columns, "name")
	if name == "" {
		name = generateName(address, targetType)
	}

	expectedStatuses, err := csvOptionalExpectedStatuses(record, columns, "expected_statuses")
	if err != nil {
		return models.Target{}, err
	}

	return models.Target{
		Name:             name,
		Type:             targetType,
		Protocol:         targetType,
		Address:          address,
		IntervalSec:      interval,
		TimeoutSec:       timeout,
		Retries:          retries,
		RetryDelayMs:     retryDelayMS,
		Resolver:         resolver,
		ExpectedStatuses: expectedStatuses,
	}, nil
}

// csvOptionalExpectedStatuses parses a semicolon-separated list of HTTP status
// codes from the CSV record column name. Returns nil if the column is absent
// or empty.
func csvOptionalExpectedStatuses(record []string, columns map[string]int, name string) ([]int, error) {
	value := csvValue(record, columns, name)
	if value == "" {
		return nil, nil
	}
	parts := strings.Split(value, ";")
	var out []int
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("%s must be semicolon-separated integers, got %q", name, value)
		}
		out = append(out, n)
	}
	return out, nil
}

func targetFromLine(line string, opts ImportOptions) (models.Target, error) {
	targetType := opts.DefaultType
	address := line

	switch {
	case strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://"):
		targetType = "http-head"
	case isHostPort(line):
		targetType = "tcp"
	default:
		switch opts.DefaultType {
		case "http", "http-head":
			address = "https://" + line
		case "dns":
			address = line
		}
	}

	return models.Target{
		Name:         generateName(address, targetType),
		Type:         targetType,
		Protocol:     targetType,
		Address:      address,
		IntervalSec:  opts.Interval,
		TimeoutSec:   opts.Timeout,
		Retries:      opts.Retries,
		RetryDelayMs: opts.RetryDelayMS,
	}, nil
}

func isAllowedDefaultType(value string) bool {
	switch value {
	case "http", "http-head", "tcp", "dns":
		return true
	default:
		return false
	}
}

func isHostPort(value string) bool {
	host, port, err := net.SplitHostPort(value)
	if err == nil {
		return host != "" && port != "" && isPort(port)
	}

	lastColon := strings.LastIndex(value, ":")
	if lastColon <= 0 || lastColon == len(value)-1 {
		return false
	}
	if strings.Contains(value[:lastColon], ":") {
		return false
	}
	return isPort(value[lastColon+1:])
}

func csvValue(record []string, columns map[string]int, name string) string {
	index, ok := columns[name]
	if !ok || index >= len(record) {
		return ""
	}
	return strings.TrimSpace(record[index])
}

func csvOptionalInt(record []string, columns map[string]int, name string, fallback int) (int, error) {
	value := csvValue(record, columns, name)
	if value == "" {
		return fallback, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	return parsed, nil
}

func isEmptyCSVRecord(record []string) bool {
	for _, value := range record {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}

func isPort(value string) bool {
	port, err := strconv.Atoi(value)
	return err == nil && port > 0 && port <= 65535
}

// normalizeExpectedStatuses converts expected_statuses into a canonical string
// representation for duplicate detection. Returns "default" if nil/empty,
// or a comma-separated sorted list of statuses.
func normalizeExpectedStatuses(statuses []int) string {
	if len(statuses) == 0 {
		return "default"
	}
	// Create a copy and sort to ensure [200,403] and [403,200] match
	sorted := make([]int, len(statuses))
	copy(sorted, statuses)
	slices.Sort(sorted)

	// Convert to comma-separated string
	parts := make([]string, len(sorted))
	for i, code := range sorted {
		parts[i] = strconv.Itoa(code)
	}
	return strings.Join(parts, ",")
}

func dedupeKey(target models.Target) string {
	// For HTTP/http-head, include normalized expected_statuses in the key
	if target.Type == "http" || target.Type == "http-head" {
		statusKey := normalizeExpectedStatuses(target.ExpectedStatuses)
		return target.Type + "\x00" + target.Address + "\x00" + target.Resolver + "\x00" + statusKey
	}
	// For tcp/dns, use the same format with a normalized (empty) status key
	statusKey := normalizeExpectedStatuses(nil)
	return target.Type + "\x00" + target.Address + "\x00" + target.Resolver + "\x00" + statusKey
}

func generateName(address, targetType string) string {
	host, port := nameHostPort(address, targetType)
	base := displayHost(host)
	switch targetType {
	case "http":
		return base + " HTTP"
	case "http-head":
		return base + " HTTP HEAD"
	case "tcp":
		if port != "" {
			return base + " TCP " + port
		}
		return base + " TCP"
	case "dns":
		return base + " DNS"
	default:
		return base + " " + strings.ToUpper(targetType)
	}
}

func nameHostPort(address, targetType string) (string, string) {
	if targetType == "http" || targetType == "http-head" {
		parsed, err := url.Parse(address)
		if err == nil && parsed.Host != "" {
			return parsed.Hostname(), parsed.Port()
		}
	}

	if host, port, err := net.SplitHostPort(address); err == nil {
		return host, port
	}

	lastColon := strings.LastIndex(address, ":")
	if lastColon > 0 && lastColon < len(address)-1 && !strings.Contains(address[:lastColon], ":") {
		return address[:lastColon], address[lastColon+1:]
	}

	return address, ""
}

func displayHost(host string) string {
	host = strings.TrimSpace(strings.TrimSuffix(host, "."))
	host = strings.TrimPrefix(strings.ToLower(host), "www.")
	if host == "" {
		return "Target"
	}

	label := strings.Split(host, ".")[0]
	special := map[string]string{
		"github":     "GitHub",
		"gitlab":     "GitLab",
		"httpbin":    "HTTPBin",
		"cloudflare": "Cloudflare",
	}
	if value, ok := special[label]; ok {
		return value
	}

	words := strings.FieldsFunc(label, func(r rune) bool {
		return r == '-' || r == '_'
	})
	for i, word := range words {
		if word == "" {
			continue
		}
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}

func printDryRun(added, skipped []models.Target, total int) {
	fmt.Fprintln(os.Stdout, "Targets to add:")
	if len(added) == 0 {
		fmt.Fprintln(os.Stdout, "  (none)")
	}
	for _, target := range added {
		statusKey := normalizeExpectedStatuses(target.ExpectedStatuses)
		fmt.Fprintf(os.Stdout, "  - name=%q type=%s protocol=%s address=%s resolver=%s expected_statuses=%s\n",
			target.Name, target.Type, target.Protocol, target.Address, target.Resolver, statusKey)
	}

	fmt.Fprintln(os.Stdout, "Skipped duplicates:")
	if len(skipped) == 0 {
		fmt.Fprintln(os.Stdout, "  (none)")
	}
	for _, target := range skipped {
		statusKey := normalizeExpectedStatuses(target.ExpectedStatuses)
		fmt.Fprintf(os.Stdout, "  - name=%q type=%s address=%s resolver=%s expected_statuses=%s\n",
			target.Name, target.Type, target.Address, target.Resolver, statusKey)
	}

	fmt.Fprintf(os.Stdout, "Total targets after import: %d\n", total)
}

func writeBackup(path string, data []byte, mode os.FileMode) (string, error) {
	backupPath := path + ".bak." + time.Now().Format("20060102-150405")
	if err := os.MkdirAll(filepath.Dir(backupPath), 0755); err != nil {
		return "", fmt.Errorf("creating backup directory: %w", err)
	}
	if err := os.WriteFile(backupPath, data, mode); err != nil {
		return "", fmt.Errorf("writing backup config: %w", err)
	}
	return backupPath, nil
}
