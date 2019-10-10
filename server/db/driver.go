package db

// var (
// 	driverMtx  sync.Mutex
// 	driverName string
// 	driver     Driver
// )

// type Driver interface {
// 	Open(ctx context.Context, cfg interface{}) (DEXArchivist, error)
// }

// func Register(name string, d Driver) {
// 	driverMtx.Lock()
// 	defer driverMtx.Unlock()
// 	if d == nil {
// 		panic("db: Register driver is nil")
// 	}
// 	if driver != nil {
// 		panic("db: Register already called. driver: " + driverName)
// 	}
// 	driverName = name
// 	driver = d
// }
