// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pg

import (
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"decred.org/dcrdex/server/db/driver/pg/internal"
	_ "github.com/lib/pq" // Start the PostgreSQL sql driver
)

const publicSchema = "public"

// connect opens a connection to a PostgreSQL database. The caller is
// responsible for calling Close() on the returned db when finished using it.
// The input host may be an IP address for TCP connection, or an absolute path
// to a UNIX domain socket. An empty string should be provided for UNIX sockets.
func connect(host, port, user, pass, dbName string) (*sql.DB, error) {
	var psqlInfo string
	if pass == "" {
		psqlInfo = fmt.Sprintf("host=%s user=%s "+
			"dbname=%s sslmode=disable",
			host, user, dbName)
	} else {
		psqlInfo = fmt.Sprintf("host=%s user=%s "+
			"password=%s dbname=%s sslmode=disable",
			host, user, pass, dbName)
	}

	// Only add port for a TCP connection since UNIX domain sockets (specified
	// by a "/" prefix) do not have a port.
	if !strings.HasPrefix(host, "/") {
		psqlInfo += fmt.Sprintf(" port=%s", port)
	}

	db, err := sql.Open("postgres", psqlInfo)
	if err != nil {
		return nil, err
	}

	// Establish a connection and verify it is alive.
	err = db.Ping()
	return db, err
}

// sqlExecutor is implemented by both sql.DB and sql.Tx.
type sqlExecutor interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
}

// sqlExec executes the SQL statement string with any optional arguments, and
// returns the number of rows affected.
func sqlExec(db sqlExecutor, stmt string, args ...interface{}) (int64, error) {
	res, err := db.Exec(stmt, args...)
	if err != nil {
		return 0, err
	}
	if res == nil {
		return 0, nil
	}

	var N int64
	N, err = res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf(`error in RowsAffected: %v`, err)
	}
	return N, err
}

// sqlExecStmt executes the prepared SQL statement with any optional arguments,
// and returns the number of rows affected.
func sqlExecStmt(stmt *sql.Stmt, args ...interface{}) (int64, error) {
	res, err := stmt.Exec(args...)
	if err != nil {
		return 0, err
	}
	if res == nil {
		return 0, nil
	}

	var N int64
	N, err = res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf(`error in RowsAffected: %v`, err)
	}
	return N, err
}

// namespacedTableExists checks if the specified table exists.
func namespacedTableExists(db *sql.DB, schema, tableName string) (bool, error) {
	rows, err := db.Query(`SELECT 1
		FROM   pg_tables
		WHERE  schemaname = $1
		AND    tablename = $2;`,
		schema, tableName)
	if err != nil {
		return false, err
	}

	defer func() {
		if e := rows.Close(); e != nil {
			log.Errorf("Close of Query failed: %v", e)
		}
	}()
	return rows.Next(), nil
}

// tableExists checks if the specified table exists.
func tableExists(db *sql.DB, tableName string) (bool, error) {
	rows, err := db.Query(`select relname from pg_class where relname = $1`,
		tableName)
	if err != nil {
		return false, err
	}

	defer func() {
		if e := rows.Close(); e != nil {
			log.Errorf("Close of Query failed: %v", e)
		}
	}()
	return rows.Next(), nil
}

// schemaExists checks if the specified schema exists.
func schemaExists(db *sql.DB, tableName string) (bool, error) {
	rows, err := db.Query(`select 1 from pg_catalog.pg_namespace where nspname = $1`,
		tableName)
	if err != nil {
		return false, err
	}

	defer func() {
		if e := rows.Close(); e != nil {
			log.Errorf("Close of Query failed: %v", e)
		}
	}()
	return rows.Next(), nil
}

// createTable creates a table with the given name using the provided SQL
// statement, if it does not already exist.
func createTable(db *sql.DB, fmtStmt, schema, tableName string) (bool, error) {
	exists, err := namespacedTableExists(db, schema, tableName)
	if err != nil {
		return false, err
	}

	nameSpacedTable := schema + "." + tableName
	var created bool
	if !exists {
		stmt := fmt.Sprintf(fmtStmt, nameSpacedTable)
		log.Infof(`Creating the "%s" table.`, nameSpacedTable)
		_, err = db.Exec(stmt)
		if err != nil {
			return false, err
		}
		created = true
	} else {
		log.Tracef(`Table "%s" exists.`, nameSpacedTable)
	}

	return created, err
}

func dropTable(db sqlExecutor, tableName string) error {
	_, err := db.Exec(fmt.Sprintf(`DROP TABLE IF EXISTS %s;`, tableName))
	return err
}

// existsIndex checks if the specified index name exists.
func existsIndex(db *sql.DB, indexName string) (exists bool, err error) {
	err = db.QueryRow(internal.IndexExists, indexName, publicSchema).Scan(&exists)
	if err == sql.ErrNoRows {
		err = nil
	}
	return
}

// isUniqueIndex checks if the given index name is defined as UNIQUE.
func isUniqueIndex(db *sql.DB, indexName string) (isUnique bool, err error) {
	err = db.QueryRow(internal.IndexIsUnique, indexName, publicSchema).Scan(&isUnique)
	return
}

// parseUnit is used to separate a "unit" from pg_settings such as "8kB" into a
// numeric component and a base unit string.
func parseUnit(unit string) (multiple float64, baseUnit string, err error) {
	// This regular expression is defined so that it will match any input.
	re := regexp.MustCompile(`([-\d\.]*)\s*(.*)`)
	matches := re.FindStringSubmatch(unit)
	// One or more of the matched substrings may be "", but the base unit
	// substring (matches[2]) will match anything.
	if len(matches) != 3 {
		panic("inconceivable!")
	}

	// The regexp eats leading spaces, but there may be trailing spaces
	// remaining that should be removed.
	baseUnit = strings.TrimSuffix(matches[2], " ")

	// The numeric component is processed by strconv.ParseFloat except in the
	// cases of an empty string or a single "-", which is interpreted as a
	// negative sign.
	switch matches[1] {
	case "":
		multiple = 1
	case "-":
		multiple = -1
	default:
		multiple, err = strconv.ParseFloat(matches[1], 64)
		if err != nil {
			// If the numeric part does not parse as a valid number (e.g.
			// "3.2.1-"), reset the base unit and return the non-nil error.
			baseUnit = ""
		}
	}

	return
}

// PGSetting describes a PostgreSQL setting scanned from pg_settings.
type PGSetting struct {
	Name, Setting, Unit, ShortDesc, Source, SourceFile, SourceLine string
}

// PGSettings facilitates looking up a PGSetting based on a setting's Name.
type PGSettings map[string]PGSetting

// String implements the Stringer interface, generating a table of the settings
// where the Setting and Unit fields are merged into a single column. The rows
// of the table are sorted by the PGSettings string key (the setting's Name).
// This function is not thread-safe, so do not modify PGSettings concurrently.
func (pgs PGSettings) String() string {
	// Sort the names.
	numSettings := len(pgs)
	names := make([]string, 0, numSettings)
	for name := range pgs {
		names = append(names, name)
	}
	sort.Strings(names)

	// Determine max width of "Setting", "Name", and "File" entries.
	fileWidth, nameWidth, settingWidth := 4, 4, 7
	// Also combine Setting and Unit, in the same order as the sorted names.
	fullSettings := make([]string, 0, numSettings)
	for i := range names {
		s, ok := pgs[names[i]]
		if !ok {
			log.Errorf("(PGSettings).String is not thread-safe!")
			continue
		}

		// Combine Setting and Unit.
		fullSetting := s.Setting
		// See if setting is numeric. Assume non-numeric settings have no Unit.
		if num1, err := strconv.ParseFloat(s.Setting, 64); err == nil {
			// Combine with the unit if numeric.
			if num2, unit, err := parseUnit(s.Unit); err == nil {
				if unit != "" {
					unit = " " + unit
				}
				// Combine. e.g. 10.0, "8kB" => "80 kB"
				fullSetting = fmt.Sprintf("%.12g%s", num1*num2, unit)
			} else {
				// Mystery unit.
				fullSetting += " " + s.Unit
			}
		}

		fullSettings = append(fullSettings, fullSetting)

		if len(fullSetting) > settingWidth {
			settingWidth = len(fullSetting)
		}

		// File column width.
		if len(s.SourceFile) > fileWidth {
			fileWidth = len(s.SourceFile)
		}
		// Name column width.
		if len(s.Name) > nameWidth {
			nameWidth = len(s.Name)
		}
	}

	format := "%" + strconv.Itoa(nameWidth) + "s | %" + strconv.Itoa(settingWidth) +
		"s | %10.10s | %" + strconv.Itoa(fileWidth) + "s | %5s | %-48.48s\n"

	// Write the headers and a horizontal bar.
	out := fmt.Sprintf(format, "Name", "Setting", "Source", "File", "Line", "Description")
	hBar := strings.Repeat(string([]rune{0x2550}), nameWidth+1) + string([]rune{0x256A}) +
		strings.Repeat(string([]rune{0x2550}), settingWidth+2) + string([]rune{0x256A}) +
		strings.Repeat(string([]rune{0x2550}), 12) + string([]rune{0x256A}) +
		strings.Repeat(string([]rune{0x2550}), fileWidth+2) + string([]rune{0x256A}) +
		strings.Repeat(string([]rune{0x2550}), 7) + string([]rune{0x256A}) +
		strings.Repeat(string([]rune{0x2550}), 50)
	out += hBar + "\n"

	// Write each row.
	for i := range names {
		s, ok := pgs[names[i]]
		if !ok {
			log.Warnf("(PGSettings).String is not thread-safe!")
			continue
		}
		out += fmt.Sprintf(format, s.Name, fullSettings[i], s.Source,
			s.SourceFile, s.SourceLine, s.ShortDesc)
	}
	return out
}

// retrievePGVersion retrieves the version of the connected PostgreSQL server.
func retrievePGVersion(db *sql.DB) (ver string, err error) {
	err = db.QueryRow(internal.RetrievePGVersion).Scan(&ver)
	return
}

// retrieveSysSettings retrieves the PostgreSQL settings provided a query that
// returns the following columns from pg_setting in order: name, setting, unit,
// short_desc, source, sourcefile, sourceline.
func retrieveSysSettings(stmt string, db *sql.DB) (PGSettings, error) {
	rows, err := db.Query(stmt)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	settings := make(PGSettings)

	for rows.Next() {
		var name, setting, unit, shortDesc, source, sourceFile sql.NullString
		var sourceLine sql.NullInt64
		err = rows.Scan(&name, &setting, &unit, &shortDesc,
			&source, &sourceFile, &sourceLine)
		if err != nil {
			return nil, err
		}

		// If the source is "configuration file", but the file path is empty,
		// the connected postgres user does not have sufficient privileges.
		var line, file string
		if source.String == "configuration file" {
			// Shorten the source string.
			source.String = "conf file"
			if sourceFile.String == "" {
				file = "NO PERMISSION"
			} else {
				file = sourceFile.String
				line = strconv.FormatInt(sourceLine.Int64, 10)
			}
		}

		settings[name.String] = PGSetting{
			Name:       name.String,
			Setting:    setting.String,
			Unit:       unit.String,
			ShortDesc:  shortDesc.String,
			Source:     source.String,
			SourceFile: file,
			SourceLine: line,
		}
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return settings, nil
}

// retrieveSysSettingsConfFile retrieves settings that are set by a
// configuration file (rather than default, environment variable, etc.).
func retrieveSysSettingsConfFile(db *sql.DB) (PGSettings, error) {
	return retrieveSysSettings(internal.RetrieveSysSettingsConfFile, db)
}

// retrieveSysSettingsPerformance retrieves performance-related settings.
func retrieveSysSettingsPerformance(db *sql.DB) (PGSettings, error) {
	return retrieveSysSettings(internal.RetrieveSysSettingsPerformance, db)
}

// retrieveSysSettingsServer a key server configuration settings (config_file,
// data_directory, max_connections, dynamic_shared_memory_type,
// max_files_per_process, port, unix_socket_directories), which may be helpful
// in debugging connectivity issues or other DB errors.
func retrieveSysSettingsServer(db *sql.DB) (PGSettings, error) {
	return retrieveSysSettings(internal.RetrieveSysSettingsServer, db)
}

// retrieveSysSettingSyncCommit retrieves the synchronous_commit setting.
func retrieveSysSettingSyncCommit(db *sql.DB) (syncCommit string, err error) {
	err = db.QueryRow(internal.RetrieveSyncCommitSetting).Scan(&syncCommit)
	return
}

// setSynchronousCommit sets the synchronous_commit setting.
func setSynchronousCommit(db sqlExecutor, syncCommit string) error {
	_, err := db.Exec(fmt.Sprintf(`SET synchronous_commit TO %s;`, syncCommit))
	return err
}

// checkCurrentTimeZone queries for the currently set postgres time zone.
func checkCurrentTimeZone(db *sql.DB) (currentTZ string, err error) {
	if err = db.QueryRow(`SHOW TIME ZONE`).Scan(&currentTZ); err != nil {
		err = fmt.Errorf("unable to query current time zone: %v", err)
	}
	return
}

func (a *Archiver) checkPerfSettings(hidePGConfig bool) error {
	// Optionally log the PostgreSQL configuration.
	if !hidePGConfig {
		perfSettings, err := retrieveSysSettingsPerformance(a.db)
		if err != nil {
			return err
		}
		log.Infof("postgres configuration settings:\n%v", perfSettings)

		servSettings, err := retrieveSysSettingsServer(a.db)
		if err != nil {
			return err
		}
		log.Infof("postgres server settings:\n%v", servSettings)
	}

	// Check the synchronous_commit setting.
	syncCommit, err := retrieveSysSettingSyncCommit(a.db)
	if err != nil {
		return err
	}
	if syncCommit != "off" {
		log.Warnf(`PERFORMANCE ISSUE! The synchronous_commit setting is "%s". `+
			`Changing it to "off".`, syncCommit)
		// Turn off synchronous_commit.
		if err = setSynchronousCommit(a.db, "off"); err != nil {
			return fmt.Errorf("failed to set synchronous_commit: %v", err)
		}
		// Verify that the setting was changed.
		if syncCommit, err = retrieveSysSettingSyncCommit(a.db); err != nil {
			return err
		}
		if syncCommit != "off" {
			return fmt.Errorf(`Failed to set synchronous_commit="off". ` +
				`Check PostgreSQL user permissions.`)
		}
	}
	return nil
}

// createSchema creates a new schema.
func createSchema(db *sql.DB, schema string) (bool, error) {
	exists, err := schemaExists(db, schema)
	if err != nil {
		return false, err
	}

	var created bool
	if !exists {
		log.Infof(`Creating schema "%s".`, schema)
		stmt := fmt.Sprintf(internal.CreateSchema, schema)
		_, err = db.Exec(stmt)
		if err != nil {
			return false, err
		}
		created = true
	} else {
		log.Tracef(`Schema "%s" exists.`, schema)
	}

	return created, err
}

// fullTableName creates a long-form table name of the form dbName.schema.table.
func fullTableName(dbName, schema, table string) string {
	return dbName + "." + schema + "." + table
}
