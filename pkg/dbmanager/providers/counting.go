package providers

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"sync/atomic"
)

// countingConnector wraps a driver.Connector and counts every physical
// connection it successfully dials. sql.DBStats has no such field, so this is
// the only way to report the total number of connections ever opened.
type countingConnector struct {
	driver.Connector
	opened *atomic.Int64
}

func (c *countingConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err == nil {
		c.opened.Add(1)
	}
	return conn, err
}

// dsnConnector adapts a plain driver.Driver to driver.Connector.
type dsnConnector struct {
	dsn string
	drv driver.Driver
}

func (c dsnConnector) Connect(context.Context) (driver.Conn, error) { return c.drv.Open(c.dsn) }
func (c dsnConnector) Driver() driver.Driver                        { return c.drv }

// openCounted is sql.Open with every dialled connection counted in opened.
func openCounted(driverName, dsn string, opened *atomic.Int64) (*sql.DB, error) {
	probe, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, err
	}
	drv := probe.Driver()
	probe.Close() //nolint:gosec // G104: probe handle never dialled

	var connector driver.Connector = dsnConnector{dsn: dsn, drv: drv}
	if dc, ok := drv.(driver.DriverContext); ok {
		if connector, err = dc.OpenConnector(dsn); err != nil {
			return nil, err
		}
	}
	return sql.OpenDB(&countingConnector{Connector: connector, opened: opened}), nil
}
