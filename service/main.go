// File: main.go
// Package main implements a Modbus TCP server that collects boolean field data
// and writes it to InfluxDB only if the data has changed.
package main

import (
	"log"
	"os"
	"os/exec"
	"strconv"
	"time"

	"vtarchitect/api"
	"vtarchitect/config"
	"vtarchitect/data"
	"vtarchitect/influx"

	"path/filepath"

	"github.com/tbrandon/mbserver"
)

// collectChangedFields recursively collects changed fields into the changed map.
func collectChangedFields(curr, prev map[string]interface{}, changed map[string]interface{}, prefix string) {
	for k, v := range curr {
		if pv, ok := prev[k]; ok {
			switch va := v.(type) {
			case map[string]interface{}:
				bv, ok := pv.(map[string]interface{})
				if ok {
					// Only add prefix for nested maps (not float groups)
					collectChangedFields(va, bv, changed, prefix+k+".")
				}
			case map[string]float32:
				bv, ok := pv.(map[string]float32)
				if ok {
					for fk, fv := range va {
						if bv[fk] != fv {
							// Use fk as the full field name (do not prefix with group)
							changed[fk] = fv
						}
					}
				}
			default:
				fullKey := k
				if prefix != "" {
					fullKey = prefix + k
				}
				if va != pv {
					changed[fullKey] = va
				}
			}
		} else {
			// New key
			switch va := v.(type) {
			case map[string]interface{}:
				collectChangedFields(va, map[string]interface{}{}, changed, prefix+k+".")
			case map[string]float32:
				for fk, fv := range va {
					changed[fk] = fv
				}
			default:
				fullKey := k
				if prefix != "" {
					fullKey = prefix + k
				}
				changed[fullKey] = va
			}
		}
	}
}

// processAndLogChangedYAML writes only changed fields to InfluxDB using the YAML-driven map, recursively.
func processAndLogChangedYAML(cfg *config.Config, plcData, prev map[string]interface{}, batchWriter *influx.ChannelBatchWriter) {
	measurement := cfg.Values["INFLUXDB_MEASUREMENT"]
	if measurement == "" {
		measurement = "status_data"
	}
	changed := make(map[string]interface{})
	collectChangedFields(plcData, prev, changed, "")
	if len(changed) == 0 {
		return // nothing to write
	}
	batchWriter.AddPoint(measurement, nil, changed, time.Now())
	log.Printf("INFLUX: Buffered changed fields for InfluxDB: %s", changed)
}

// getPollInterval retrieves the polling interval from the configuration.
func getPollInterval(cfg *config.Config) time.Duration {
	pollInterval := cfg.Values["PLC_POLL_MS"]
	pollIntervalMs, err := strconv.Atoi(pollInterval)
	if err != nil || pollIntervalMs <= 0 {
		pollIntervalMs = 1000 // default to 1 second
	}
	return time.Duration(pollIntervalMs) * time.Millisecond
}

// getFullWriteInterval retrieves the full-state write interval from the configuration (in minutes).
func getFullWriteInterval(cfg *config.Config) time.Duration {
	intervalStr := cfg.Values["FULL_WRITE_MINUTES"]
	intervalMin, err := strconv.Atoi(intervalStr)
	if err != nil || intervalMin <= 0 {
		intervalMin = 60 // default to 60 minutes
	}
	return time.Duration(intervalMin) * time.Minute
}

// processAndLogFullYAML writes the full PLC state to InfluxDB using the YAML-driven map.
func processAndLogFullYAML(cfg *config.Config, plcData map[string]interface{}, batchWriter *influx.ChannelBatchWriter) {
	measurement := cfg.Values["INFLUXDB_MEASUREMENT"]
	if measurement == "" {
		measurement = "status_data"
	}
	batchWriter.AddPoint(measurement, nil, plcData, time.Now())
	log.Println("INFLUX: Buffered full-state write for InfluxDB")
}

// runEthernetIPCycle connects to the PLC via Ethernet/IP and continuously polls for data changes.
func runEthernetIPCycle(cfg *config.Config, batchWriter *influx.ChannelBatchWriter) {
	ip := cfg.Values["ETHERNET_IP_ADDRESS"]
	eth := data.NewPLC(ip)

	for {
		err := eth.Connect()
		if err != nil {
			log.Printf("DATA: PLC connection failed, retrying in 5 seconds: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}
		break
	}
	defer eth.Disconnect()

	pollInterval := getPollInterval(cfg)
	fullWriteInterval := getFullWriteInterval(cfg)
	fullWriteTicker := time.NewTicker(fullWriteInterval)
	defer fullWriteTicker.Stop()
	var last map[string]interface{}
	for {
		plcData, err := data.LoadFromEthernetIP(cfg, eth)
		if err != nil {
			log.Printf("DATA:Error loading PLC data from Ethernet/IP YAML: %v", err)
			time.Sleep(pollInterval)
			continue
		}
		select {
		case <-fullWriteTicker.C:
			processAndLogFullYAML(cfg, plcData, batchWriter)
			last = plcData
		default:
			if !mapsEqual(last, plcData) {
				processAndLogChangedYAML(cfg, plcData, last, batchWriter)
				last = plcData
			}
		}
		time.Sleep(pollInterval)
	}
}

// mapsEqual recursively compares two map[string]interface{} for equality, including nested maps.
func mapsEqual(a, b map[string]interface{}) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		bv, ok := b[k]
		if !ok {
			return false
		}
		switch va := v.(type) {
		case map[string]interface{}:
			bvMap, ok := bv.(map[string]interface{})
			if !ok || !mapsEqual(va, bvMap) {
				return false
			}
		case map[string]float32:
			bvMap, ok := bv.(map[string]float32)
			if !ok || !float32MapsEqual(va, bvMap) {
				return false
			}
		default:
			if va != bv {
				return false
			}
		}
	}
	return true
}

// float32MapsEqual compares two map[string]float32 for equality.
func float32MapsEqual(a, b map[string]float32) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

// runModbusCycle connects to the Modbus TCP server and continuously polls for data changes.
func runModbusCycle(cfg *config.Config, server *mbserver.Server, batchWriter *influx.ChannelBatchWriter) {
	startStr := cfg.Values["MODBUS_REGISTER_START"]
	endStr := cfg.Values["MODBUS_REGISTER_END"]
	start, err := strconv.Atoi(startStr)
	if err != nil {
		log.Fatalf("FATAL: Invalid MODBUS_REGISTER_START: %v", err)
	}
	end, err := strconv.Atoi(endStr)
	if err != nil {
		log.Fatalf("FATAL: Invalid MODBUS_REGISTER_END: %v", err)
	}

	pollInterval := getPollInterval(cfg)
	fullWriteInterval := getFullWriteInterval(cfg)
	fullWriteTicker := time.NewTicker(fullWriteInterval)
	defer fullWriteTicker.Stop()
	var last map[string]interface{}
	for {
		if len(server.HoldingRegisters) <= end {
			log.Println("DATA: Insufficient register length, skipping cycle")
			time.Sleep(5 * time.Second)
			continue
		}
		readSlice := server.HoldingRegisters[start : end+1]
		plcData, err := data.ParsePLCDataFromRegisters(readSlice)
		if err != nil {
			log.Printf("ERROR: Error loading PLC data from Modbus YAML: %v", err)
			time.Sleep(pollInterval)
			continue
		}
		select {
		case <-fullWriteTicker.C:
			processAndLogFullYAML(cfg, plcData, batchWriter)
			last = plcData
		default:
			if !mapsEqual(last, plcData) {
				processAndLogChangedYAML(cfg, plcData, last, batchWriter)
				last = plcData
			}
		}
		time.Sleep(pollInterval)
	}
}

// checkAndConvertCSV checks for a CSV in tools/csv-to-yaml, converts it to YAML, and deletes the CSV
func checkAndConvertCSV() error {
	sharedDir := "../shared"
	yamlPath := filepath.Join(sharedDir, "architect.yaml")

	log.Println("STARTUP: Checking for CSV file in shared directory...")
	files, err := filepath.Glob(filepath.Join(sharedDir, "*.csv"))
	if err != nil {
		log.Printf("[ERROR] Could not search for CSV: %v", err)
		return err
	}
	if len(files) == 0 {
		log.Println("STARTUP: No CSV file found. Skipping conversion.")
		return nil // No CSV to process
	}

	csvPath := files[0] // Use the first CSV found
	log.Printf("STARTUP: Found CSV: %s. Converting to YAML...", csvPath)
	cmd := exec.Command("go", "run", filepath.Join(sharedDir, "csv-to-yaml.go"), csvPath, yamlPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Printf("[ERROR] CSV to YAML conversion failed: %v", err)
		return err
	}

	log.Printf("STARTUP: Conversion complete. Deleting CSV: %s", csvPath)
	if err := os.Remove(csvPath); err != nil {
		log.Printf("[ERROR] Could not delete CSV: %v", err)
		return err
	}
	log.Printf("STARTUP: Converted %s to %s and deleted the CSV.", csvPath, yamlPath)

	return nil
}

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("FATAL: Failed to load config: %v", err)
	}

	log.Println("STARTUP: Checking for CSV and converting to YAML if present...")
	if err := checkAndConvertCSV(); err != nil {
		log.Fatalf("FATAL: CSV to YAML conversion failed: %v", err)
	}

	log.Println("STARTUP: Loading and caching architect.yaml...")
	err = data.LoadAndCacheArchitectYAML("../shared/architect.yaml")
	if err != nil {
		log.Fatalf("FATAL: Failed to load architect.yaml: %v", err)
	}
	log.Println("STARTUP: architect.yaml loaded and cached successfully.")

	plcSource := cfg.Values["PLC_DATA_SOURCE"]
	log.Printf("STARTUP: PLC data source: %s", plcSource)

	influxClient, err := influx.NewClient(cfg)
	if err != nil {
		log.Fatalf("FATAL: Failed to connect to InfluxDB: %v", err)
	}
	defer influxClient.Close()

	batchWriter := influx.NewChannelBatchWriter(influxClient.GetWriteAPI(), 100)
	defer batchWriter.Close()

	go api.StartAPIServer(cfg, influxClient)

	if plcSource == "ethernet-ip" {
		runEthernetIPCycle(cfg, batchWriter)
	} else {
		server := mbserver.NewServer()
		port := cfg.Values["MODBUS_TCP_PORT"]
		if port == "" {
			port = "5020"
		}
		err := server.ListenTCP("0.0.0.0:" + port)
		if err != nil {
			log.Fatalf("FATAL: Failed to start Modbus server: %v", err)
		}
		defer server.Close()
		log.Printf("DATA: Modbus server listening on port %s", port)

		runModbusCycle(cfg, server, batchWriter)
	}
}
