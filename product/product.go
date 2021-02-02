package product

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
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
	lock      RedisLock
}

func NewProductInfo(id int) *productInfo {
	product := &productInfo{
		productId: id,
		redisKey:  fmt.Sprintf("openapi:product:%d", id),
		lock:      RedisLock{fmt.Sprintf("openapi:lock:%d", id), 1},
	}

	return product
}

func (pro *productInfo) Get() (string, error) {
	key := pro.redisKey
	//读缓存
	client := redis.GetClient()
	val, err := client.Get(key).Result() //1.正常值，err == nil; 2.没有key，读到空，err=redis.Nil; 3.读到默认值，"*"; 4.redis出问题，得到err
	if err != nil {
		//缓存没有，读db

		//避免缓存击穿，并发只有一个去请求db，其余的坐享其成
		//如果err!=nil，lock会一直重试，达到阻塞的效果
		//如果redis连不上等错误，err!=nil，后面会去db取数据
		if err = pro.lock.Lock(); err == nil {
			defer pro.lock.Unlock()
		}

		//如果是第一个拿到锁，再次读缓存，仍然没有，则去db取，然后放到缓存
		//如果是后面等待的线程拿到锁，第一个释放锁之前，已经将数据数据放到缓存，所以后面的拿到锁的时候，缓存已经有数据了
		val, err := client.Get(key).Result()
		if err != nil {
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

func (pro *productInfo) Del() error {
	return nil
}

//商品详情有更新或新增，收到消息都会去db取最新的数据，更新到redis
//如果更新失败，上游程序会将失败消息重发消息队列，重试
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

type RedisLock struct {
	key    string
	expire int
}

//获取锁成功，返回nil
//获取锁失败，一直重试，直到成功，返回nil
//其他错误，直接返回error
func (l RedisLock) Lock() error {
	client := redis.GetClient()
	for {
		ok, err := client.SetNX(l.key, 1, time.Duration(l.expire)*time.Second).Result()
		if err != nil {
			log.Println(err)
			return err
		}

		if ok {
			return nil
		}

		time.Sleep(time.Duration(10) * time.Millisecond)
	}
}

//重试3次
//如果最后都失败，只能等超时释放
func (l RedisLock) Unlock() {
	client := redis.GetClient()
	for i := 0; i < 3; i++ {
		res, err := client.Del(l.key).Result()
		if err == nil && res > 0 {
			break
		}
	}
}
