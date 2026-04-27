package Database

import (
	"database/sql"
	"log"
)

func ConnectToDB() *sql.DB {
	db, err := sql.Open("mysql", "snap:REDACTED@tcp(<legacy-db-host>:3306)/Falcon")
	if err != nil {
		log.Println(err)
	}
	return db
}
