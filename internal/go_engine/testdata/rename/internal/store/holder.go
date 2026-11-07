package store

import "reflect"

// holder's field is read by reflect under its name, so it keeps its name.
type holder struct {
	UserID string
}

// User reads the holder's field by name.
func User() string {
	return reflect.ValueOf(holder{UserID: "user"}).FieldByName("UserID").String()
}
