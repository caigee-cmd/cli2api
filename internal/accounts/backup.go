package accounts

import "time"

type Backup struct {
	Name      string    `json:"name"`
	Path      string    `json:"-"`
	CreatedAt time.Time `json:"created_at"`
}
