package product

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"openapi/stores/mysql"
	"openapi/stores/redis"
	"sync"
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
	lock      sync.Mutex
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
			log.Println(err)
			return "", err
		}

		//缓存没有，读db
		//避免缓存击穿，并发只有一个去请求db，其余的坐享其成
		pro.lock.Lock()
		defer pro.lock.Unlock()
		//如果是第一个拿到锁，再次读缓存，仍然没有，则去db取，然后放到缓存
		//如果是后面等待的线程拿到锁，第一个释放锁之前，已经将数据数据放到缓存，所以后面的拿到锁的时候，缓存已经有数据了
		val, err := client.Get(key).Result()
		if err != nil {
			if err != redis.Nil {
				log.Println(err)
				return "", err
			}

			product := &Product{Pid: pro.productId}
			db := mysql.GetDb()
			has, err := db.Get(product)
			if err != nil {
				log.Println(err)
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
			productStr, err := json.Marshal(product)
			if err != nil {
				log.Println(err)
				return "", err
			}
			val = string(productStr)
			rand.Seed(time.Now().Unix())
			expiration := expire + rand.Int()
			if err = client.Set(key, val, time.Duration(expiration)*time.Second).Err(); err != nil {
				log.Println(err)
			}
			return val, nil
		}
	}

	//读到默认值
	if val == placeHolder {
		err = errors.New("not found")
		log.Println(err)
		return "", err
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
		log.Println(err)
		return err
	}
	if has == false {
		err = errors.New("not found")
		log.Println(err)
		return err
	}

	productStr, err := json.Marshal(product)
	if err != nil {
		log.Println(err)
		return err
	}
	val := string(productStr)
	key := pro.redisKey
	rand.Seed(time.Now().Unix())
	expiration := expire + rand.Int()
	client := redis.GetClient()
	if err = client.Set(key, val, time.Duration(expiration)*time.Second).Err(); err != nil {
		log.Println(err)
		return err
	}

	return nil
}
