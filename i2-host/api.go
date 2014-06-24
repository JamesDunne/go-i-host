package main

import (
	"database/sql"
	"fmt"
	_ "github.com/JamesDunne/go-util/base"
	_ "github.com/JamesDunne/go-util/db/sqlite3"
	"github.com/JamesDunne/go-util/db/sqlx"
)

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
		//api.ddl(
		//	`pragma user_version = 3`,
		//)
		//userVersion = "3"
	}

	return
}

func (api *API) Close() {
	api.db.Close()
}

type Image struct {
	ID             int64
	Kind           string
	Title          string
	SourceURL      *string
	CollectionName string `db:"CollectionName"`
	Submitter      string `db:"Submitter"`
	RedirectToID   *int64
	IsHidden       bool
	IsClean        bool
}

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
}

func mapRecToEnt(r *dbImage) *Image {
	return &Image{
		ID:           r.ID,
		Kind:         r.Kind,
		Title:        r.Title,
		SourceURL:    nullStringToPtr(r.SourceURL),
		RedirectToID: nullInt64ToPtr(r.RedirectToID),
		IsHidden:     int64ToBool(r.IsHidden),
		IsClean:      int64ToBool(r.IsClean),
	}
}

const nonIDColumns = "Kind, Title, SourceURL, RedirectToID, IsHidden, IsClean"

func (api *API) NewImage(img *Image) (int64, error) {
	var query string
	var args []interface{}
	if img.ID <= 0 {
		// Insert a new record:
		query = `insert into Image (` + nonIDColumns + `) values (?1, ?2, ?3, ?4, ?5, ?6)`
		args = []interface{}{img.Kind, img.Title, ptrToNullString(img.SourceURL), ptrToNullInt64(img.RedirectToID), boolToInt64(img.IsHidden), boolToInt64(img.IsClean)}
	} else {
		// Do an identity insert:
		query = `insert into Image (ID, ` + nonIDColumns + `) values (?1, ?2, ?3, ?4, ?5, ?6, ?7)`
		args = []interface{}{img.ID, img.Kind, img.Title, ptrToNullString(img.SourceURL), ptrToNullInt64(img.RedirectToID), boolToInt64(img.IsHidden), boolToInt64(img.IsClean)}
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

func (api *API) GetImage(id int64) (img *Image, err error) {
	rec := new(dbImage)
	err = api.db.Get(rec, `select ID, `+nonIDColumns+` from Image where ID = ?1`, id)
	if err == sql.ErrNoRows {
		rec = nil
		return nil, nil
	}
	img = mapRecToEnt(rec)
	return
}

type ImagesOrderBy int

const (
	ImagesOrderByTitleASC  ImagesOrderBy = iota
	ImagesOrderByTitleDESC ImagesOrderBy = iota
	ImagesOrderByIDASC     ImagesOrderBy = iota
	ImagesOrderByIDDESC    ImagesOrderBy = iota
)

func (api *API) GetList(orderBy ImagesOrderBy) (imgs []Image, err error) {
	recs := make([]dbImage, 0, 200)
	switch orderBy {
	case ImagesOrderByTitleASC:
		err = api.db.Select(&recs, `select ID, `+nonIDColumns+` from Image order by Title ASC`)
	case ImagesOrderByTitleDESC:
		err = api.db.Select(&recs, `select ID, `+nonIDColumns+` from Image order by Title DESC`)
	case ImagesOrderByIDASC:
		err = api.db.Select(&recs, `select ID, `+nonIDColumns+` from Image order by ID ASC`)
	case ImagesOrderByIDDESC:
		err = api.db.Select(&recs, `select ID, `+nonIDColumns+` from Image order by ID DESC`)
	}
	if err != nil {
		return
	}

	imgs = make([]Image, len(recs))
	for i, r := range recs {
		// FIXME(jsd): Use better non-copying behavior.
		imgs[i] = *mapRecToEnt(&r)
	}
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
