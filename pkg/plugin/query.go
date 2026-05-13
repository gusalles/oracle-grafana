package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
	go_ora "github.com/sijms/go-ora/v2"
)

type OracleDatasourceQuery struct {
	Datasource   OracleDatasourceInfo
	DatasourceId int64
	IntervalMs   int64
	O_parsed     string
	O_sql        string
	RefId        string
}

type OracleDatasourceInfo struct {
	Type string
	Uid  string
}

type OracleDatasourceColumn struct {
	name     string
	dataType string
	values   []any
}

type OracleDatasourceResult struct {
	err     error
	columns []OracleDatasourceColumn
}

func (q *OracleDatasourceQuery) MakeQuery(c *OracleDatasourceConnection, from time.Time, to time.Time) (result OracleDatasourceResult) {
	result = OracleDatasourceResult{nil, []OracleDatasourceColumn{}}

	// Recover from any panics in the go-ora driver to prevent crashing the plugin process
	defer func() {
		if r := recover(); r != nil {
			log.DefaultLogger.Error("Recovered from panic during query execution", "panic", fmt.Sprintf("%v", r), "query", q.O_parsed)
			result.err = fmt.Errorf("internal error during query execution: %v", r)
			// Mark connection as broken so the next request will reconnect
			c.connection = nil
		}
	}()

	if c.connection == nil {
		result.err = fmt.Errorf("database connection is not available")
		log.DefaultLogger.Error("Query attempted on disconnected database")
		return result
	}

	// Create a context with timeout for the query (30 seconds default)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	drRows, err := c.connection.QueryContext(ctx, q.O_parsed, nil)
	if err != nil {
		log.DefaultLogger.Error("Error querying SQL: ", err)
		result.err = err
		return result
	}

	rows, ok := drRows.(*go_ora.DataSet)
	if !ok {
		result.err = fmt.Errorf("unexpected rows type: %T", drRows)
		return result
	}
	defer rows.Close()

	columnNames := rows.Columns()
	typeMap := make(map[string]string)
	for i, name := range columnNames {
		typename := GetDataTypeByType(rows.ColumnTypeScanType(i))
		log.DefaultLogger.Debug(fmt.Sprintf("column: %v, dataType:%v", name, typename))
		typeMap[name] = typename
		result.columns = append(result.columns, OracleDatasourceColumn{name, typename, []any{}})
	}
	log.DefaultLogger.Debug("Oracle query fetch: ", "columns", columnNames)

	scannedValues := make([]interface{}, len(columnNames))
	scanArgs := make([]interface{}, len(columnNames))
	for i := range scanArgs {
		scanArgs[i] = &scannedValues[i]
	}

	for rows.Next_() {
		err := rows.Scan(scanArgs...)
		if err != nil {
			log.DefaultLogger.Error("Error scanning row: ", err)
			break
		}
		for index, val := range scannedValues {
			if val != nil {
				dataType := typeMap[result.columns[index].name]
				convertedValue := ConvertNativeValue(val, dataType)
				result.columns[index].values = append(result.columns[index].values, convertedValue)
			} else {
				result.columns[index].values = append(result.columns[index].values, nil)
			}
		}
	}

	if rows.Err() != nil {
		result.err = rows.Err()
		log.DefaultLogger.Error("Error fetching rows: ", rows.Err())
	}

	log.DefaultLogger.Debug("Oracle query: ", "result", result)
	return result
}

func (q *OracleDatasourceQuery) ParseDatasourceQuery(query backend.DataQuery) error {
	log.DefaultLogger.Debug("backend query", "json", query.JSON)
	err := json.Unmarshal(query.JSON, &q)
	if err != nil {
		log.DefaultLogger.Error("Error parsing Oracle query: ", err)
	}
	return err
}
