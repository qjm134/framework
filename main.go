package main

import (
	"openapi/stores/mysql"
	"openapi/stores/redis"
)

func main() {
	redis.Init()
	mysql.Init()

}
