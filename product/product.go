package product

import (
	"errors"
	"fmt"
	"openapi/stores/redis"
)

const placeHolder = "*"

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

func (product *productInfo) Get() (string, error) {
	key := product.redisKey
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

		//db有数据，设置缓存
		//没有数据设默认值，下次访问缓存可以读到，防止雪崩，并设过期时间
	}

	//读到默认值
	if val == placeHolder {
		return "", errors.New("not found")
	}

	//缓存命中
	return val, nil
}
