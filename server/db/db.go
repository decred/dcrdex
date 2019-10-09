package db

// import (
// 	"sync"

// 	"github.com/decred/dcrdex/server/db/driver"
// )

// var (
// 	driversMu sync.Mutex
// 	drivers   = make(map[string]driver.Driver)
// )

// // nowFunc returns the current time; it's overridden in tests.
// var nowFunc = time.Now

// // Register makes a database driver available by the provided name.
// // If Register is called twice with the same name or if driver is nil,
// // it panics.
// func Register(driver driver.Driver) {
// 	driversMu.Lock()
// 	defer driversMu.Unlock()
// 	if driver == nil {
// 		panic("db: Register driver is nil")
// 	}
// 	if _, dup := drivers[name]; dup {
// 		panic("sql: Register called twice for driver " + name)
// 	}
// 	drivers[name] = driver
// }
