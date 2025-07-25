# VTArchitect Service

A backend service for industrial data acquisition, processing, and visualization. It connects to Programmable Logic Controllers (PLCs) via Modbus TCP or Ethernet/IP, interprets the data using a flexible YAML configuration, logs it to InfluxDB for historical analysis, and exposes a RESTful API.

## Features

*   **Dual Protocol Support**: Acts as a Modbus TCP slave to receive data or as an Ethernet/IP client to poll data from Allen-Bradley PLCs.
*   **Configuration-Driven**: Leverages a central `architect.yaml` file to map raw PLC register/tag data to meaningful field names and types (booleans, faults, floats).
*   **Efficient Time-Series Logging**: Intelligently detects data changes and only writes new data points to InfluxDB, minimizing storage and network overhead. A periodic full-state write ensures data synchronization.
*   **Batch Writing**: Buffers data points and writes them to InfluxDB in batches for improved performance.
*   **REST API**: Provides endpoints to query aggregated statistics (e.g., uptime percentages, fault counts, average values) and detailed time-series data for frontend applications.
*   **Dynamic Mapping Updates**: Supports updating the core `architect.yaml` mapping by uploading a CSV file via an API endpoint, which is automatically converted on service startup.

## Project Structure

A quick overview of the directories within the `service`:
*   `/api`: Contains the web server logic and REST API endpoint handlers.
*   `/config`: Handles loading environment variables from the `.env` file.
*   `/data`: Manages PLC communication (Modbus, Ethernet/IP) and data parsing based on `architect.yaml`.
*   `/influx`: Provides the client for interacting with InfluxDB, including writing points and executing Flux queries.
*   `/main.go`: The main application entry point, responsible for initialization and orchestrating the different components.

## Prerequisites

*   Go 1.24.2 or later
*   An active InfluxDB 2.x instance
*   A PLC accessible over the network that supports either:
    *   Modbus TCP (PLC acts as master)
    *   Ethernet/IP (for Allen-Bradley/Rockwell PLCs)

## Configuration

The service is configured using a `.env` file located in the project root (one level above the `service` directory, i.e., `../.env`).

#### Core Settings

-   `PLC_DATA_SOURCE`: The protocol to use. Set to `ethernet-ip` or `modbus`. Defaults to `modbus` if not set.
-   `PLC_POLL_MS`: The data polling interval in milliseconds. (Default: `1000`)
-   `FULL_WRITE_MINUTES`: The interval in minutes for a full data state write to InfluxDB. (Default: `60`)

#### Modbus TCP Settings (if `PLC_DATA_SOURCE=modbus`)

-   `MODBUS_TCP_PORT`: The TCP port for the Modbus server to listen on. (Default: `5020`)
-   `MODBUS_REGISTER_START`: The starting address of the holding registers to read data from.
-   `MODBUS_REGISTER_END`: The ending address of the holding registers.

#### Ethernet/IP Settings (if `PLC_DATA_SOURCE=ethernet-ip`)

-   `ETHERNET_IP_ADDRESS`: The IP address of the target PLC.
-   `ETHERNET_IP_LENGTH`: The length of the integer array tag to read from the PLC. (Default: `100`)

#### InfluxDB Settings

-   `INFLUXDB_URL`: The URL of your InfluxDB instance (e.g., `http://localhost:8086`).
-   `INFLUXDB_TOKEN`: The authentication token for InfluxDB.
-   `INFLUXDB_ORG`: The InfluxDB organization name.
-   `INFLUXDB_BUCKET`: The InfluxDB bucket to write data to.
-   `INFLUXDB_MEASUREMENT`: The measurement name for the data points. (Default: `status_data`)

## The `architect.yaml` File

This file is the heart of the service's data mapping logic. It defines how raw data from PLC registers or tags is translated into named fields. The service expects this file to be at `../shared/architect.yaml`.

**Structure:**
```yaml
# ../shared/architect.yaml

boolean_fields:
  - name: "SystemStatusBits.MachineRunning"
    address: 0
    bit: 0
  - name: "SystemStatusBits.InAutoMode"
    address: 0
    bit: 1

fault_fields:
  - name: "FaultBits.EStopPressed"
    address: 10
    bit: 0
  - name: "FaultBits.MotorOverload"
    address: 10
    bit: 1

float_fields:
  - name: "Floats.MotorSpeed(HighINT)"
    address: 20
  - name: "Floats.MotorSpeed(LowINT)"
    address: 21
  - name: "Floats.ProductTemp(HighINT)"
    address: 22
  - name: "Floats.ProductTemp(LowINT)"
    address: 23
```

-   **`boolean_fields` & `fault_fields`**: Map a specific `bit` within a register at `address` to a boolean field `name`.
-   **`float_fields`**: Map two 16-bit integer registers (`address`) to a single 32-bit float value. The service automatically pairs fields with `(HighINT)` and `(LowINT)` suffixes on the same base name.

### Dynamic Updates via CSV

The service provides a convenient way to manage this mapping:
1.  **Upload**: A user can upload a specially formatted CSV file (e.g., `go-import-tag.csv`) to the `/api/upload-csv` endpoint.
2.  **Placement**: The file is saved in the `../shared/` directory.
3.  **Conversion**: On the next startup, the service detects the `.csv` file, runs a conversion script (`../shared/csv-to-yaml.go`) to generate a new `architect.yaml`, and then deletes the source CSV.

## How It Works

1.  **Initialization**: On startup, the service loads configuration from `.env`, checks for and converts any pending CSV files, and loads the final `architect.yaml` into an in-memory cache for fast access.
2.  **Data Source Branching**: Based on the `PLC_DATA_SOURCE` variable, it enters one of two main loops.
3.  **Modbus TCP Mode**:
    -   The service starts a Modbus TCP server that listens for incoming connections from a PLC.
    -   It assumes the PLC is configured as a Modbus Master and is actively writing data to the service's holding registers.
    -   At a regular interval (`PLC_POLL_MS`), the service reads its own holding register block, parses it using the `architect.yaml` mapping, and compares it to the last known state.
4.  **Ethernet/IP Mode**:
    -   The service acts as a client, connecting to the specified Allen-Bradley PLC.
    -   At a regular interval, it reads a predefined integer array tag (hardcoded as `EthernetDataWrite`).
    -   This array is treated as a block of registers and is parsed using the same `architect.yaml` mapping.
5.  **Logging**: In both modes, if the parsed data has changed since the last poll, only the changed fields are written as a new point to InfluxDB. A full data snapshot is written periodically (`FULL_WRITE_MINUTES`) to ensure data consistency.

## API Endpoints

The service hosts a REST API on port `:8080`.

*   **`GET /`**
    -   Serves the static files for the frontend web application, located in `../console/dist`.

*   **`POST /api/upload-csv`**
    -   Uploads a new CSV mapping file.
    -   **Form Data**: `file`: The CSV file to upload.

*   **`GET /api/stats`**
    -   Retrieves a comprehensive set of aggregated statistics for the specified time range.
    -   **Query Parameters**:
        -   `start` (optional): The start of the time range in Flux format (e.g., `-1h`, `-7d`) or RFC3339 timestamp. (Default: `-1h`)
        -   `stop` (optional): The end of the time range. (Default: `now()`)
        -   `bucket` (optional): The InfluxDB bucket to query. (Defaults to `INFLUXDB_BUCKET` from config).
    -   **Response Body**:
        ```json
        {
          "boolean_percentages": {
            "SystemStatusBits.MachineRunning": 95.5,
            "SystemStatusBits.InAutoMode": 80.1
          },
          "fault_counts": {
            "FaultBits.EStopPressed": 2.0,
            "FaultBits.MotorOverload": 5.0
          },
          "float_averages": {
            "Floats.MotorSpeed": 1750.25,
            "Floats.ProductTemp": 45.7
          }
        }
        ```

*   **`GET /api/float-range`**
    -   Retrieves raw time-series data for a single float field. Useful for plotting graphs.
    -   **Query Parameters**:
        -   `field`: The name of the float field to query (e.g., `Floats.MotorSpeed`). **Required**.
        -   `start`, `stop`, `bucket`: Same as `/api/stats`.
    -   **Response Body**:
        ```json
        [
          { "time": "2023-10-27T10:00:00Z", "value": 1750.0 },
          { "time": "2023-10-27T10:01:00Z", "value": 1751.5 },
          ...
        ]
        ```

*   **`GET /api/percentages`**
    -   A legacy endpoint that retrieves only the boolean percentage statistics. `/api/stats` is recommended for new integrations.

## Running the Service

1.  **Navigate to the service directory:**
    ```bash
    cd service
    ```

2.  **Install dependencies:**
    ```bash
    go mod tidy
    ```

3.  **Create and configure the environment file:**
    -   Create a file named `.env` in the project root (`vtarchitect/`).
    -   Populate it with the configuration variables described above.

4.  **Run the application:**
    ```bash
    go run main.go
    ```
    The service will start, connect to the data source, and begin listening for API requests on `http://localhost:8080`.

## Key Dependencies

The service is built on several key open-source libraries:
-   gologix: For Ethernet/IP communication.
-   mbserver: For the Modbus TCP server implementation.
-   influxdb-client-go: The official InfluxDB 2.x Go client.
-   godotenv: For loading environment variables.