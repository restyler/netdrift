package main

import (
	"encoding/json"
	"testing"
	"time"
)

// TestUpstreamTagging tests the upstream tagging functionality
func TestUpstreamTagging(t *testing.T) {
	t.Run("BasicTagSupport", func(t *testing.T) {
		config := &Config{
			Server: struct {
				Name          string `json:"name"`
				ListenAddress string `json:"listen_address"`
				StatsEndpoint string `json:"stats_endpoint"`
			}{
				Name:          "Test Proxy",
				ListenAddress: "127.0.0.1:3180",
				StatsEndpoint: "/stats",
			},
			UpstreamProxies: []struct {
				URL     string `json:"url"`
				Enabled bool   `json:"enabled"`
				Weight  int    `json:"weight"`
			Tag     string `json:"tag,omitempty"`
		Note    string `json:"note,omitempty"`
			}{
				{URL: "http://127.0.0.1:9100", Enabled: true, Weight: 1, Tag: "aws-us-east"},
				{URL: "http://127.0.0.1:9101", Enabled: true, Weight: 1, Tag: "aws-us-east"},
				{URL: "http://127.0.0.1:9102", Enabled: true, Weight: 1, Tag: "gcp-us-central"},
				{URL: "http://127.0.0.1:9103", Enabled: true, Weight: 1, Tag: ""},
			},
		}

		ps := NewProxyServer(config, "")

		// Check that tags are properly stored in weighted upstreams
		awsCount := 0
		gcpCount := 0
		untaggedCount := 0

		for _, weighted := range ps.weightedUpstreams {
			switch weighted.Tag {
			case "aws-us-east":
				awsCount++
			case "gcp-us-central":
				gcpCount++
			case "":
				untaggedCount++
			}
		}

		if awsCount != 2 {
			t.Errorf("Expected 2 AWS upstreams, got %d", awsCount)
		}
		if gcpCount != 1 {
			t.Errorf("Expected 1 GCP upstream, got %d", gcpCount)
		}
		if untaggedCount != 1 {
			t.Errorf("Expected 1 untagged upstream, got %d", untaggedCount)
		}

		// Check that tags are stored in health tracking
		for url, health := range ps.upstreamHealth {
			expectedTag := ""
			for _, weighted := range ps.weightedUpstreams {
				if weighted.URL == url {
					expectedTag = weighted.Tag
					break
				}
			}
			if health.Tag != expectedTag {
				t.Errorf("Health tracking tag mismatch for %s: expected %q, got %q", url, expectedTag, health.Tag)
			}
		}

		// Check that tags are stored in stats
		for url, stats := range ps.stats.UpstreamMetrics {
			expectedTag := ""
			for _, weighted := range ps.weightedUpstreams {
				if weighted.URL == url {
					expectedTag = weighted.Tag
					break
				}
			}
			if stats.Tag != expectedTag {
				t.Errorf("Stats tag mismatch for %s: expected %q, got %q", url, expectedTag, stats.Tag)
			}
		}
	})

	t.Run("TaggedHealthManagement", func(t *testing.T) {
		config := &Config{
			UpstreamProxies: []struct {
				URL     string `json:"url"`
				Enabled bool   `json:"enabled"`
				Weight  int    `json:"weight"`
			Tag     string `json:"tag,omitempty"`
		Note    string `json:"note,omitempty"`
			}{
				{URL: "http://127.0.0.1:9104", Enabled: true, Weight: 1, Tag: "provider-a"},
				{URL: "http://127.0.0.1:9105", Enabled: true, Weight: 1, Tag: "provider-a"},
				{URL: "http://127.0.0.1:9106", Enabled: true, Weight: 1, Tag: "provider-b"},
			},
		}

		ps := NewProxyServer(config, "")

		// Simulate health events
		ps.recordUpstreamFailure("http://127.0.0.1:9104")
		ps.recordUpstreamFailure("http://127.0.0.1:9104")
		ps.recordUpstreamFailure("http://127.0.0.1:9104") // Should trigger unhealthy
		ps.recordUpstreamSuccess("http://127.0.0.1:9105")
		ps.recordUpstreamSuccess("http://127.0.0.1:9106")

		// Check health metrics with tag grouping
		healthMetrics := ps.getHealthMetrics()

		// Verify individual upstream health includes tags
		upstreams, ok := healthMetrics["upstreams"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected upstreams in health metrics")
		}

		upstream104, ok := upstreams["http://127.0.0.1:9104"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected upstream 9104 in health metrics")
		}
		if upstream104["tag"] != "provider-a" {
			t.Errorf("Expected tag 'provider-a', got %v", upstream104["tag"])
		}
		if upstream104["healthy"] != false {
			t.Errorf("Expected upstream 9104 to be unhealthy")
		}

		// Verify tag groups are present
		tagGroups, ok := healthMetrics["tag_groups"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected tag_groups in health metrics")
		}

		providerA, ok := tagGroups["provider-a"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected provider-a in tag groups")
		}
		if providerA["total_upstreams"] != 2 {
			t.Errorf("Expected 2 upstreams for provider-a, got %v", providerA["total_upstreams"])
		}
		if providerA["healthy_upstreams"] != 1 {
			t.Errorf("Expected 1 healthy upstream for provider-a, got %v", providerA["healthy_upstreams"])
		}
		if providerA["unhealthy_upstreams"] != 1 {
			t.Errorf("Expected 1 unhealthy upstream for provider-a, got %v", providerA["unhealthy_upstreams"])
		}
	})

	t.Run("TaggedStatistics", func(t *testing.T) {
		config := &Config{
			UpstreamProxies: []struct {
				URL     string `json:"url"`
				Enabled bool   `json:"enabled"`
				Weight  int    `json:"weight"`
			Tag     string `json:"tag,omitempty"`
		Note    string `json:"note,omitempty"`
			}{
				{URL: "http://127.0.0.1:9107", Enabled: true, Weight: 1, Tag: "region-east"},
				{URL: "http://127.0.0.1:9108", Enabled: true, Weight: 1, Tag: "region-west"},
			},
		}

		ps := NewProxyServer(config, "")

		// Simulate some recent requests
		ps.mutex.Lock()
		ps.stats.RecentRequests = append(ps.stats.RecentRequests, []struct {
			Timestamp time.Time
			Upstream  string
			Latency   int64
			Success   bool
		}{
			{Timestamp: time.Now(), Upstream: "http://127.0.0.1:9107", Latency: 100, Success: true},
			{Timestamp: time.Now(), Upstream: "http://127.0.0.1:9107", Latency: 200, Success: true},
			{Timestamp: time.Now(), Upstream: "http://127.0.0.1:9108", Latency: 150, Success: true},
			{Timestamp: time.Now(), Upstream: "http://127.0.0.1:9108", Latency: 300, Success: false},
		}...)
		ps.mutex.Unlock()

		// Get time window stats
		stats := ps.getTimeWindowStats(15 * time.Minute)

		// Verify tag groups are included in stats
		if stats.TagGroups == nil {
			t.Fatal("Expected tag groups in time window stats")
		}

		if len(stats.TagGroups) != 2 {
			t.Errorf("Expected 2 tag groups, got %d", len(stats.TagGroups))
		}

		regionEast, exists := stats.TagGroups["region-east"]
		if !exists {
			t.Fatal("Expected region-east in tag groups")
		}
		if regionEast.TotalRequests != 2 {
			t.Errorf("Expected 2 requests for region-east, got %d", regionEast.TotalRequests)
		}
		if regionEast.SuccessRequests != 2 {
			t.Errorf("Expected 2 successful requests for region-east, got %d", regionEast.SuccessRequests)
		}

		regionWest, exists := stats.TagGroups["region-west"]
		if !exists {
			t.Fatal("Expected region-west in tag groups")
		}
		if regionWest.TotalRequests != 2 {
			t.Errorf("Expected 2 requests for region-west, got %d", regionWest.TotalRequests)
		}
		if regionWest.SuccessRequests != 1 {
			t.Errorf("Expected 1 successful request for region-west, got %d", regionWest.SuccessRequests)
		}
		if regionWest.FailedRequests != 1 {
			t.Errorf("Expected 1 failed request for region-west, got %d", regionWest.FailedRequests)
		}
	})

	t.Run("TaggedLoadBalancing", func(t *testing.T) {
		config := &Config{
			UpstreamProxies: []struct {
				URL     string `json:"url"`
				Enabled bool   `json:"enabled"`
				Weight  int    `json:"weight"`
			Tag     string `json:"tag,omitempty"`
		Note    string `json:"note,omitempty"`
			}{
				{URL: "http://127.0.0.1:9109", Enabled: true, Weight: 3, Tag: "high-performance"},
				{URL: "http://127.0.0.1:9110", Enabled: true, Weight: 1, Tag: "backup"},
				{URL: "http://127.0.0.1:9111", Enabled: true, Weight: 0, Tag: "maintenance"}, // Zero weight
			},
		}

		ps := NewProxyServer(config, "")

		// Test load balancing respects weights regardless of tags
		upstreamCounts := make(map[string]int)
		tagCounts := make(map[string]int)

		for i := 0; i < 100; i++ {
			upstream := ps.getNextUpstream()
			upstreamCounts[upstream]++

			// Find the tag for this upstream
			for _, weighted := range ps.weightedUpstreams {
				if weighted.URL == upstream {
					tagCounts[weighted.Tag]++
					break
				}
			}
		}

		// Zero weight upstream should not be selected
		if count, exists := upstreamCounts["http://127.0.0.1:9111"]; exists && count > 0 {
			t.Errorf("Zero weight upstream (maintenance tag) was selected %d times", count)
		}

		// High-performance tag should get ~75% of traffic (3/4 weight ratio)
		// Backup tag should get ~25% of traffic (1/4 weight ratio)
		highPerfCount := tagCounts["high-performance"]
		backupCount := tagCounts["backup"]
		maintenanceCount := tagCounts["maintenance"]

		if maintenanceCount > 0 {
			t.Errorf("Maintenance tag should not receive any traffic, got %d", maintenanceCount)
		}

		// Check approximate ratio (allowing 15% tolerance)
		totalTraffic := highPerfCount + backupCount
		if totalTraffic != 100 {
			t.Errorf("Expected 100 total requests, got %d", totalTraffic)
		}

		expectedHighPerf := 75 // ~75% for weight 3
		expectedBackup := 25   // ~25% for weight 1
		tolerance := 15        // 15% tolerance

		if absInt(highPerfCount-expectedHighPerf) > tolerance {
			t.Errorf("High-performance tag: expected %d±%d, got %d", expectedHighPerf, tolerance, highPerfCount)
		}
		if absInt(backupCount-expectedBackup) > tolerance {
			t.Errorf("Backup tag: expected %d±%d, got %d", expectedBackup, tolerance, backupCount)
		}
	})
}

// TestConfigurationWithTags tests configuration parsing with tags
func TestConfigurationWithTags(t *testing.T) {
	t.Run("JSONConfigurationWithTags", func(t *testing.T) {
		configJSON := `{
			"server": {
				"name": "Test Proxy",
				"listen_address": "127.0.0.1:3130",
				"stats_endpoint": "/stats"
			},
			"authentication": {
				"enabled": false
			},
			"upstream_proxies": [
				{
					"url": "http://proxy1.example.com:8080",
					"enabled": true,
					"weight": 2,
					"tag": "datacenter-east"
				},
				{
					"url": "http://proxy2.example.com:8080",
					"enabled": true,
					"weight": 3,
					"tag": "datacenter-west"
				},
				{
					"url": "http://proxy3.example.com:8080",
					"enabled": true,
					"weight": 1
				}
			]
		}`

		var config Config
		err := json.Unmarshal([]byte(configJSON), &config)
		if err != nil {
			t.Fatalf("Failed to parse JSON config: %v", err)
		}

		// Verify tags are parsed correctly
		if len(config.UpstreamProxies) != 3 {
			t.Fatalf("Expected 3 upstream proxies, got %d", len(config.UpstreamProxies))
		}

		expectedTags := []string{"datacenter-east", "datacenter-west", ""}
		for i, proxy := range config.UpstreamProxies {
			if proxy.Tag != expectedTags[i] {
				t.Errorf("Proxy %d: expected tag %q, got %q", i, expectedTags[i], proxy.Tag)
			}
		}

		// Test creating proxy server with tagged config
		ps := NewProxyServer(&config, "")

		// Verify tags are properly initialized
		for _, weighted := range ps.weightedUpstreams {
			found := false
			for _, configProxy := range config.UpstreamProxies {
				if configProxy.URL == weighted.URL {
					if configProxy.Tag != weighted.Tag {
						t.Errorf("Tag mismatch for %s: config has %q, weighted has %q",
							weighted.URL, configProxy.Tag, weighted.Tag)
					}
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Weighted upstream %s not found in config", weighted.URL)
			}
		}
	})

	t.Run("TagOmissionInJSON", func(t *testing.T) {
		// Test that omitted tags work correctly (should be empty string)
		configJSON := `{
			"server": {"name": "Test", "listen_address": "127.0.0.1:3131", "stats_endpoint": "/stats"},
			"authentication": {"enabled": false},
			"upstream_proxies": [
				{"url": "http://example.com:8080", "enabled": true, "weight": 1}
			]
		}`

		var config Config
		err := json.Unmarshal([]byte(configJSON), &config)
		if err != nil {
			t.Fatalf("Failed to parse JSON config: %v", err)
		}

		if config.UpstreamProxies[0].Tag != "" {
			t.Errorf("Expected empty tag when omitted, got %q", config.UpstreamProxies[0].Tag)
		}
	})
}

// TestTaggedLogging tests that tags appear in log messages
func TestTaggedLogging(t *testing.T) {
	// This test would require capturing log output, which is complex in Go
	// For now, we'll just verify the tag information is available in the structures
	t.Run("LoggingDataStructures", func(t *testing.T) {
		config := &Config{
			UpstreamProxies: []struct {
				URL     string `json:"url"`
				Enabled bool   `json:"enabled"`
				Weight  int    `json:"weight"`
			Tag     string `json:"tag,omitempty"`
		Note    string `json:"note,omitempty"`
			}{
				{URL: "http://127.0.0.1:9112", Enabled: true, Weight: 1, Tag: "test-provider"},
			},
		}

		ps := NewProxyServer(config, "")

		// Verify that tag information is accessible for logging
		for _, weighted := range ps.weightedUpstreams {
			if weighted.Tag == "" {
				t.Error("Expected tag to be available for logging")
			}
		}

		for _, health := range ps.upstreamHealth {
			if health.Tag == "" {
				t.Error("Expected tag to be available in health tracking for logging")
			}
		}

		// The actual logging output would need to be captured and tested
		// in integration tests or with log capture mechanisms
	})
}

// Helper function for absolute value (reused from other test files)
func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}