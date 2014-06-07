package main

import (
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

func NewAPI() (api *API, err error) {
	db, err := sqlx.Open("sqlite3", db_path)
	if err != nil {
		db.Close()
		return nil, err
	}

	api = &API{db: db}

	// Set up the schema:
	api.ddl(`
create table if not exists Image (
	ID INTEGER PRIMARY KEY AUTOINCREMENT,
	MimeType TEXT NOT NULL,
	Title TEXT NOT NULL,
	CONSTRAINT PK_Image PRIMARY KEY (ID ASC)
)`)

	return
}

func (api *API) Close() {
	api.db.Close()
}

type Image struct {
	ID       int64  `db:"ID"`
	MimeType string `db:"MimeType"`
	Title    string `db:"Title"`
}

func (api *API) NewImage(img Image) (int64, error) {
	var query string
	var args []interface{}
	if img.ID <= 0 {
		// Insert a new record:
		query = `insert into Image (MimeType, Title) values (?1, ?2)`
		args = []interface{}{img.MimeType, img.Title}
	} else {
		// Do an identity insert:
		query = `insert into Image (ID, MimeType, Title) values (?1, ?2, ?3)`
		args = []interface{}{img.ID, img.MimeType, img.Title}
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

func (api *API) GetImage(id int64) (img Image, err error) {
	err = api.db.Get(&img, `select ID, MimeType, Title from Image where ID = ?1`, id)
	return
}

func (api *API) GetList() (imgs []Image, err error) {
	imgs = make([]Image, 0, 200)

	err = api.db.Select(&imgs, `select ID, MimeType, Title from Image order by Title ASC`)

	return
}
