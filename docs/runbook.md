# Operational Runbook: Aperture

## 1. Project Overview
- **Project Name:** aperture
- **Root Path:** `/Users/dshills/Development/projects/aperture`
- **Primary Languages:** Go, Shell
- **Go Module:** `github.com/dshills/aperture`

## 2. Startup Instructions
The following binary entrypoints have been detected. To start a component, navigate to the project root and use the `go run` command or build the binary.

### 2.1. Aperture (Main Service)
- **Source:** `cmd/aperture/main.go`
- **Command:**
  ```bash
  go run cmd/aperture/main.go
  ```

### 2.2. Apbench (Benchmarking Tool)
- **Source:** `cmd/apbench/main.go`
- **Command:**
  ```bash
  go run cmd/apbench/main.go
  ```

### 2.3. Apbenchfixtures (Fixture Utility)
- **Source:** `cmd/apbenchfixtures/main.go`
- **Command:**
  ```bash
  go run cmd/apbenchfixtures/main.go
  ```

### 2.4. App (Test Fixture)
- **Source:** `testdata/fixtures/small_go/cmd/app/main.go`
- **Command:**
  ```bash
  go run testdata/fixtures/small_go/cmd/app/main.go
  ```

---

## 3. Configuration (Environment Variables)
The following environment variables are required for operation. 

| Variable Key | Description | Required | Default |
| :--- | :--- | :--- | :--- |
| UNKNOWN | UNKNOWN | UNKNOWN | UNKNOWN |

*Note: No specific ConfigVar entries were detected in the fact model.*

---

## 4. External Dependencies
The following external integrations and datastores are required:

### 4.1. Datastores
- UNKNOWN

### 4.2. Third-party Integrations
- UNKNOWN

---

## 5. Operational Procedures

### 5.1. Health Checks
- UNKNOWN

### 5.2. Backup and Recovery
- UNKNOWN

### 5.3. Monitoring and Logging
- UNKNOWN

### 5.4. Scaling
- UNKNOWN

---

## 6. Security
- **Security Configuration:** UNKNOWN
- **Access Control:** UNKNOWN