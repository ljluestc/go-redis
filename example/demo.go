package main

import (
  "context"
  "fmt"
  
  "github.com/redis/go-redis/v9"
)

func main() {
  s := "gopher"
  fmt.Printf("Hello and welcome, %s!\n", s)

  // Example of using redis client
  rdb := redis.NewClient(&redis.Options{
    Addr:     "localhost:6379",
    Password: "", // no password set
    DB:       0,  // use default DB
  })

  ctx := context.Background()
  err := rdb.Set(ctx, "key", "value", 0).Err()
  if err != nil {
    panic(err)
  }

  val, err := rdb.Get(ctx, "key").Result()
  if err != nil {
    panic(err)
  }
  fmt.Println("key", val)

  for i := 1; i <= 5; i++ {
    fmt.Println("i =", 100/i)
  }
}
