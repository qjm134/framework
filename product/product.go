package product

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"openapi/stores/mysql"
	"openapi/stores/redis"
	"time"
)

const (
	placeHolder    = "*"
	expireNotFound = 24 * 3600
	expire         = 365 * 24 * 3600
)

type Product struct {
	Pid      int
	Name     string
	Describe string
	SkuId    int
}

type productInfo struct {
	productId int
	redisKey  string
}

func NewProductInfo(id int) *productInfo {
	product := &productInfo{
		productId: id,
		redisKey:  fmt.Sprintf("openapi:product:%d", id),
	}

	return product
}

func (pro *productInfo) Get() (string, error) {
	key := pro.redisKey
	//读缓存
	client := redis.GetClient()
	val, err := client.Get(key).Result() //1.正常值，err == nil; 2.没有key，读到空，redis.Nil; 3.读到默认值，"*"; 4.redis出问题，得到err
	if err != nil {
		//redis问题
		if err != redis.Nil {
			return "", err
		}

		//缓存没有，读db
		//避免缓存击穿，并发只有一个去请求db，其余的坐享其成
		product := &Product{Pid: pro.productId}
		db := mysql.GetDb()
		has, err := db.Get(product)
		if err != nil {
			return "", err
		}

		//数据库中也没有，设默认值，下次访问缓存可以读到，防止雪崩，并设过期时间
		if has == false {
			if err = client.Set(key, placeHolder, time.Duration(expireNotFound)*time.Second).Err(); err != nil {
				log.Println(err)
			}
			return "", errors.New("not found")
		}

		//db有数据，设置缓存
		productStr, _ := json.Marshal(product)
		val = string(productStr)
		expiration := time.Duration(expire) * time.Second
		if err = client.Set(key, val, expiration).Err(); err != nil {
			log.Println(err)
		}
		return val, nil
	}

	//读到默认值
	if val == placeHolder {
		return "", errors.New("not found")
	}

	//缓存命中
	return val, nil
}

func (pro *productInfo) Update() error {
	return pro.readAndWrite()
}

func (pro *productInfo) Set() error {
	return pro.readAndWrite()
}

func (pro *productInfo) readAndWrite() error {
	product := &Product{Pid: pro.productId}
	db := mysql.GetDb()
	has, err := db.Get(product)
	if err != nil {
		return err
	}
	if has == false {
		return errors.New("not found")
	}

	productStr, _ := json.Marshal(product)
	val := string(productStr)
	key := pro.redisKey
	expiration := time.Duration(expire) * time.Second
	client := redis.GetClient()
	if err = client.Set(key, val, expiration).Err(); err != nil {
		return err
	}

	return nil
}
