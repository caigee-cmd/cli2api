package doubaowork

import (
	"strconv"
	"time"
)

func nowMillis() int64 { return time.Now().UnixMilli() }

func nowSeconds() int64 { return time.Now().Unix() }

func itoa(value int) string { return strconv.Itoa(value) }
