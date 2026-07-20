package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"os"
	"sort"
	"sync"
	"time"
)

type benchOptions struct {
	Files         int
	FileSize      int64
	Concurrency   int
	Seed          int64
	Cleanup       bool
	JSON          bool
	MaxErrorRate  float64
	MinThroughput float64
}

type benchLatency struct {
	Min float64 `json:"min_ms"`
	Avg float64 `json:"avg_ms"`
	P50 float64 `json:"p50_ms"`
	P95 float64 `json:"p95_ms"`
	P99 float64 `json:"p99_ms"`
	Max float64 `json:"max_ms"`
}

type benchResult struct {
	StartedAt       string       `json:"started_at"`
	Server          string       `json:"server"`
	Namespace       string       `json:"namespace"`
	FilesRequested  int          `json:"files_requested"`
	Succeeded       int          `json:"succeeded"`
	Failed          int          `json:"failed"`
	FileSizeBytes   int64        `json:"file_size_bytes"`
	BytesUploaded   int64        `json:"bytes_uploaded"`
	Concurrency     int          `json:"concurrency"`
	Seed            int64        `json:"seed"`
	ElapsedSeconds  float64      `json:"elapsed_seconds"`
	ThroughputMiBPS float64      `json:"throughput_mib_per_second"`
	ErrorRate       float64      `json:"error_rate"`
	Latency         benchLatency `json:"latency"`
	CleanupEnabled  bool         `json:"cleanup_enabled"`
	CleanupFailed   int          `json:"cleanup_failed"`
	Errors          []string     `json:"errors,omitempty"`
}

type benchSample struct {
	fileID  string
	latency time.Duration
	err     error
}

func runBench(ctx context.Context, c *Client, opts benchOptions) (*benchResult, error) {
	result, err := executeBench(ctx, c, opts)
	if result == nil {
		return nil, err
	}
	if opts.JSON {
		encoded, marshalErr := json.MarshalIndent(result, "", "  ")
		if marshalErr != nil {
			return result, marshalErr
		}
		fmt.Fprintln(os.Stdout, string(encoded))
	} else {
		printBenchResult(result)
	}
	return result, err
}

func executeBench(ctx context.Context, c *Client, opts benchOptions) (*benchResult, error) {
	if c == nil {
		return nil, errors.New("client is required")
	}
	if opts.Files <= 0 {
		return nil, errors.New("files must be greater than zero")
	}
	if opts.FileSize <= 0 {
		return nil, errors.New("file size must be greater than zero")
	}
	if opts.FileSize > int64(int(^uint(0)>>1)) {
		return nil, errors.New("file size exceeds platform memory limit")
	}
	if opts.Concurrency <= 0 {
		return nil, errors.New("concurrency must be greater than zero")
	}
	if opts.Concurrency > opts.Files {
		opts.Concurrency = opts.Files
	}
	if opts.MaxErrorRate < 0 || opts.MaxErrorRate > 1 {
		return nil, errors.New("max error rate must be between 0 and 1")
	}

	started := time.Now().UTC()
	jobs := make(chan int)
	samples := make(chan benchSample, opts.Files)
	var workers sync.WaitGroup
	for range opts.Concurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for idx := range jobs {
				select {
				case <-ctx.Done():
					samples <- benchSample{err: ctx.Err()}
					continue
				default:
				}
				data := make([]byte, int(opts.FileSize))
				rng := rand.New(rand.NewSource(opts.Seed + int64(idx)*7919))
				_, _ = rng.Read(data)
				hash := sha256.Sum256(data)
				name := fmt.Sprintf("bench-%d-%06d.dat", opts.Seed, idx)
				start := time.Now()
				file, err := c.uploadBytes(ctx, name, data, hex.EncodeToString(hash[:]), "none", UploadOptions{
					ChunkSize:   opts.FileSize,
					Concurrency: 1,
					Compress:    "none",
					NoProgress:  true,
				})
				sample := benchSample{latency: time.Since(start), err: err}
				if file != nil {
					sample.fileID = file.FileID
				}
				samples <- sample
			}
		}()
	}
	go func() {
		defer close(jobs)
		for idx := 0; idx < opts.Files; idx++ {
			jobs <- idx
		}
	}()
	go func() {
		workers.Wait()
		close(samples)
	}()

	latencies := make([]time.Duration, 0, opts.Files)
	fileIDs := make([]string, 0, opts.Files)
	result := &benchResult{
		StartedAt:      started.Format(time.RFC3339),
		Server:         c.ServerURL,
		Namespace:      c.Namespace,
		FilesRequested: opts.Files,
		FileSizeBytes:  opts.FileSize,
		Concurrency:    opts.Concurrency,
		Seed:           opts.Seed,
		CleanupEnabled: opts.Cleanup,
	}
	for sample := range samples {
		if sample.err != nil {
			result.Failed++
			if len(result.Errors) < 20 {
				result.Errors = append(result.Errors, sample.err.Error())
			}
			continue
		}
		result.Succeeded++
		result.BytesUploaded += opts.FileSize
		latencies = append(latencies, sample.latency)
		if sample.fileID != "" {
			fileIDs = append(fileIDs, sample.fileID)
		}
	}
	uploadElapsed := time.Since(started)
	result.ElapsedSeconds = uploadElapsed.Seconds()
	if result.ElapsedSeconds > 0 {
		result.ThroughputMiBPS = float64(result.BytesUploaded) / 1024 / 1024 / result.ElapsedSeconds
	}
	result.ErrorRate = float64(result.Failed) / float64(opts.Files)
	result.Latency = summarizeBenchLatency(latencies)

	if opts.Cleanup {
		for _, fileID := range fileIDs {
			if err := c.Delete(ctx, fileID); err != nil {
				result.CleanupFailed++
				continue
			}
			if err := c.PurgeTrash(ctx, fileID); err != nil {
				result.CleanupFailed++
			}
		}
	}

	var failures []error
	if result.ErrorRate > opts.MaxErrorRate {
		failures = append(failures, fmt.Errorf("error rate %.2f%% exceeds limit %.2f%%", result.ErrorRate*100, opts.MaxErrorRate*100))
	}
	if opts.MinThroughput > 0 && result.ThroughputMiBPS < opts.MinThroughput {
		failures = append(failures, fmt.Errorf("throughput %.2f MiB/s is below limit %.2f MiB/s", result.ThroughputMiBPS, opts.MinThroughput))
	}
	if result.CleanupFailed > 0 {
		failures = append(failures, fmt.Errorf("cleanup failed for %d files", result.CleanupFailed))
	}
	return result, errors.Join(failures...)
}

func summarizeBenchLatency(values []time.Duration) benchLatency {
	if len(values) == 0 {
		return benchLatency{}
	}
	sorted := append([]time.Duration(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	var total time.Duration
	for _, value := range sorted {
		total += value
	}
	return benchLatency{
		Min: durationMillis(sorted[0]),
		Avg: durationMillis(total / time.Duration(len(sorted))),
		P50: durationMillis(benchPercentile(sorted, 0.50)),
		P95: durationMillis(benchPercentile(sorted, 0.95)),
		P99: durationMillis(benchPercentile(sorted, 0.99)),
		Max: durationMillis(sorted[len(sorted)-1]),
	}
}

func benchPercentile(sorted []time.Duration, percentile float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	index := int(math.Ceil(percentile*float64(len(sorted)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

func durationMillis(value time.Duration) float64 {
	return float64(value) / float64(time.Millisecond)
}

func printBenchResult(result *benchResult) {
	fmt.Printf("压测完成: %d/%d 成功, %d 失败\n", result.Succeeded, result.FilesRequested, result.Failed)
	fmt.Printf("  数据量: %.2f MiB\n", float64(result.BytesUploaded)/1024/1024)
	fmt.Printf("  上传耗时: %.3f 秒\n", result.ElapsedSeconds)
	fmt.Printf("  吞吐: %.2f MiB/s\n", result.ThroughputMiBPS)
	fmt.Printf("  延迟(ms): avg %.2f, p50 %.2f, p95 %.2f, p99 %.2f, max %.2f\n",
		result.Latency.Avg, result.Latency.P50, result.Latency.P95, result.Latency.P99, result.Latency.Max)
	if result.CleanupEnabled {
		fmt.Printf("  清理失败: %d\n", result.CleanupFailed)
	}
}
