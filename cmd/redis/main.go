package main

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
	"github.com/redis/go-redis/v9/example"
)

func main() {
	fmt.Println("Redis client example")
	
	// Run the example code
	example.RunExample()
	
	// Run the demo code
	example.RunDemo()
	
	// Or run your own Redis commands directly
	rdb := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "", // no password set
		DB:       0,  // use default DB
	})

	ctx := context.Background()
	err := rdb.Set(ctx, "test-key", "test-value", 0).Err()
	if err != nil {
		panic(err)
	}

	val, err := rdb.Get(ctx, "test-key").Result()
	if err != nil {
		panic(err)
	}
	fmt.Println("test-key:", val)
}
