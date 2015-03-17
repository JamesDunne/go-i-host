package main

import (
	"bytes"
	"database/sql"
	"fmt"
	_ "github.com/JamesDunne/go-util/base"
	_ "github.com/JamesDunne/go-util/db/sqlite3"
	"github.com/JamesDunne/go-util/db/sqlx"
	"strconv"
	"strings"
)

// Runes used to split words:
const wordSplitters = " \n\t:,;.-+=[]!?()$%^&*<>\"`"

// Split a string into separate words:
func splitToWords(text string) []string {
	return strings.FieldsFunc(
		text,
		func(c rune) bool { return strings.ContainsRune(wordSplitters, c) },
	)
}

func titleToKeywords(title string) string {
	return strings.Join(splitToWords(strings.ToLower(title)), " ")
}

type API struct {
	db *sqlx.DB
}

func (api *API) ddl(cmds ...string) {
	for _, cmd := range cmds {
		if _, err := api.db.Exec(cmd); err != nil {
			api.db.Close()
			panic(fmt.Errorf("%s\n%s", cmd, err))
		}
	}
}

func (api *API) userVersion() (version string, err error) {
	uvRows, err := api.db.Queryx(`pragma user_version;`)
	if err != nil {
		return
	}
	defer uvRows.Close()
	if !uvRows.Next() {
		err = fmt.Errorf("pragma user_version failed!")
		return
	}
	uv, err := uvRows.SliceScan()
	if err != nil {
		return
	}
	version = uv[0].(string)
	return
}

func NewAPI() (api *API, err error) {
	db, err := sqlx.Open("sqlite3", db_path())
	if err != nil {
		db.Close()
		return nil, err
	}

	api = &API{db: db}

	userVersion, err := api.userVersion()
	if err != nil {
		db.Close()
		return nil, err
	}

	// Set up the schema:
	if userVersion == "0" {
		api.ddl(
			`
create table if not exists Image (
	ID INTEGER PRIMARY KEY AUTOINCREMENT,
	Kind TEXT NOT NULL,
	Title TEXT NOT NULL
)`,
			`pragma user_version = 1`,
		)
		userVersion = "1"
	}
	if userVersion == "1" {
		api.ddl(
			`alter table Image add column SourceURL TEXT`,
			`alter table Image add column RedirectToID INTEGER`,
			`alter table Image add column IsHidden INTEGER NOT NULL DEFAULT 0`,
			`alter table Image add column IsClean INTEGER NOT NULL DEFAULT 0`,
			`pragma user_version = 2`,
		)
		userVersion = "2"
	}
	if userVersion == "2" {
		api.ddl(
			`alter table Image add column CollectionName TEXT NOT NULL DEFAULT ''`,
			`alter table Image add column Submitter TEXT NOT NULL DEFAULT ''`,
			`pragma user_version = 3`,
		)
		userVersion = "3"
	}
	if userVersion == "3" {
		api.ddl(
			`alter table Image add column Keywords TEXT NOT NULL DEFAULT ''`,
			`pragma user_version = 4`,
		)
		userVersion = "4"
	}

	return
}

func (api *API) Close() {
	api.db.Close()
}

// API entity
type Image struct {
	ID             int64
	Kind           string
	Title          string
	SourceURL      *string
	CollectionName string
	Submitter      string
	RedirectToID   *int64
	IsHidden       bool
	IsClean        bool
	Keywords       string
}

type columnNameSet []string

// Direct database entity (internal use only)
type dbImage struct {
	ID             int64          `db:"ID"`
	Kind           string         `db:"Kind"`
	Title          string         `db:"Title"`
	SourceURL      sql.NullString `db:"SourceURL"`
	CollectionName string         `db:"CollectionName"`
	Submitter      string         `db:"Submitter"`
	RedirectToID   sql.NullInt64  `db:"RedirectToID"`
	IsHidden       int64          `db:"IsHidden"`
	IsClean        int64          `db:"IsClean"`
	Keywords       string         `db:"Keywords"`
}

var nonIDColumnNames = []string{
	"Kind",
	"Title",
	"SourceURL",
	"CollectionName",
	"Submitter",
	"RedirectToID",
	"IsHidden",
	"IsClean",
	"Keywords",
}
var nonIDColumns = columnNameSet(nonIDColumnNames).ToCommaDelimited()

// Convert to an `[]interface{}` for passing as params to SQL query:
func (img *Image) toSQLArgs() []interface{} {
	return []interface{}{
		img.ID,
		img.Kind,
		img.Title,
		ptrToNullString(img.SourceURL),
		img.CollectionName,
		img.Submitter,
		ptrToNullInt64(img.RedirectToID),
		boolToInt64(img.IsHidden),
		boolToInt64(img.IsClean),
		img.Keywords,
	}
}

func (names columnNameSet) ToCommaDelimited() string {
	return strings.Join(names, ", ")
}

func (names columnNameSet) ToUpdateSet(startArg int) string {
	if len(names) == 0 {
		return ""
	}
	if len(names) == 1 {
		return names[0]
	}

	sep := ", "
	n := len(sep) * (len(names) - 1)
	for i := 0; i < len(names); i++ {
		n += len(names[i])
		n += (5 + (i / 10) + 1)
	}

	b := make([]byte, 0, n)
	buf := bytes.NewBuffer(b)
	buf.WriteString(names[0])
	buf.WriteString(" = ?")
	buf.WriteString(strconv.FormatInt(int64(startArg), 10))
	for i, s := range names[1:] {
		buf.WriteString(sep)
		buf.WriteString(s)
		buf.WriteString(" = ?")
		buf.WriteString(strconv.FormatInt(int64(startArg+i+1), 10))
	}
	return buf.String()
}

func mapRecToModel(r *dbImage, m *Image) *Image {
	if m == nil {
		m = &Image{}
	}
	m.ID = r.ID
	m.Kind = r.Kind
	m.Title = r.Title
	m.SourceURL = nullStringToPtr(r.SourceURL)
	m.CollectionName = r.CollectionName
	m.Submitter = r.Submitter
	m.RedirectToID = nullInt64ToPtr(r.RedirectToID)
	m.IsHidden = int64ToBool(r.IsHidden)
	m.IsClean = int64ToBool(r.IsClean)
	m.Keywords = r.Keywords
	return m
}

func (api *API) GetImage(id int64) (img *Image, err error) {
	rec := new(dbImage)
	err = api.db.Get(rec, `select ID, `+nonIDColumns+` from Image where ID = ?1`, id)
	if err == sql.ErrNoRows {
		rec = nil
		return nil, nil
	}
	img = mapRecToModel(rec, nil)
	return
}

type ImagesOrderBy int

const (
	ImagesOrderByTitleASC  ImagesOrderBy = iota
	ImagesOrderByTitleDESC ImagesOrderBy = iota
	ImagesOrderByIDASC     ImagesOrderBy = iota
	ImagesOrderByIDDESC    ImagesOrderBy = iota
)

func (orderBy ImagesOrderBy) ToSQL() string {
	var ob string
	switch orderBy {
	default:
		fallthrough
	case ImagesOrderByTitleASC:
		ob = "order by Title ASC"
	case ImagesOrderByTitleDESC:
		ob = "order by Title DESC"
	case ImagesOrderByIDASC:
		ob = "order by ID ASC"
	case ImagesOrderByIDDESC:
		ob = "order by ID DESC"
	}

	return ob
}

func (api *API) GetAll(orderBy ImagesOrderBy) (imgs []Image, err error) {
	ob := orderBy.ToSQL()

	recs := make([]dbImage, 0, 200)
	err = api.db.Select(&recs, `select ID, `+nonIDColumns+` from Image `+ob)
	if err != nil {
		return
	}

	imgs = make([]Image, len(recs))
	for i, _ := range recs {
		mapRecToModel(&recs[i], &imgs[i])
	}
	return
}

func (api *API) GetList(collectionName string, orderBy ImagesOrderBy) (imgs []Image, err error) {
	ob := orderBy.ToSQL()

	recs := make([]dbImage, 0, 200)
	err = api.db.Select(&recs, `select ID, `+nonIDColumns+` from Image where CollectionName = ?1 or CollectionName = '' `+ob, collectionName)
	if err != nil {
		return
	}

	imgs = make([]Image, len(recs))
	for i, _ := range recs {
		mapRecToModel(&recs[i], &imgs[i])
	}
	return
}

func (api *API) GetListOnly(collectionName string, orderBy ImagesOrderBy) (imgs []Image, err error) {
	ob := orderBy.ToSQL()

	recs := make([]dbImage, 0, 200)
	err = api.db.Select(&recs, `select ID, `+nonIDColumns+` from Image where CollectionName = ?1 `+ob, collectionName)
	if err != nil {
		return
	}

	imgs = make([]Image, len(recs))
	for i, _ := range recs {
		mapRecToModel(&recs[i], &imgs[i])
	}
	return
}

func (api *API) NewImage(img *Image) (int64, error) {
	var query string
	var args []interface{}
	if img.ID <= 0 {
		// Insert a new record:
		query = `insert into Image (` + nonIDColumns + `) values (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9)`
		args = img.toSQLArgs()[1:]
	} else {
		// Do an identity insert:
		query = `insert into Image (ID, ` + nonIDColumns + `) values (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10)`
		args = img.toSQLArgs()
	}

	res, err := api.db.Exec(query, args...)
	if err != nil {
		return 0, err
	}

	// Get last inserted ID:
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}

	return id, nil
}

func (api *API) Update(img *Image) error {
	var query string
	var args []interface{}

	// Update an existing record:
	query = `update Image set ` + columnNameSet(nonIDColumnNames).ToUpdateSet(2) + ` where ID = ?1`
	args = img.toSQLArgs()

	_, err := api.db.Exec(query, args...)
	if err != nil {
		return err
	}

	return nil
}

func (api *API) Delete(id int64) (err error) {
	_, err = api.db.Exec(`delete from Image where ID = ?1`, id)
	return
}

// ------

func nullInt64ToPtr(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	return &(n.Int64)
}

func nullStringToPtr(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	return &(n.String)
}

func ptrToNullInt64(v *int64) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{Valid: false}
	}
	return sql.NullInt64{Int64: *v, Valid: true}
}

func ptrToNullString(v *string) sql.NullString {
	if v == nil {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: *v, Valid: true}
}

func int64ToBool(v int64) bool {
	return v != 0
}

func boolToInt64(v bool) int64 {
	if v {
		return 1
	}
	return 0
}
