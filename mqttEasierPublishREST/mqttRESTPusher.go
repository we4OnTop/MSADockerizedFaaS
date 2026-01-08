package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

const FAASTopic = "FAASFunctions"

var ctx = context.Background()

func main() {
	RestPortEnv := os.Getenv("REST_PORT")
	RedisAddrENV := os.Getenv("REDIS_ADDR")
	AddFaasRedisTopic := os.Getenv("ADD_FAAS_REDIS_TOPIC")
	RemoveFaasRedisTopic := os.Getenv("REMOVE_FAAS_REDIS_TOPIC")

	log.Printf("RestPortEnv: %v", RestPortEnv)
	log.Printf("RedisAddrENV: %v", RedisAddrENV)
	log.Printf("AddFaasRedisTopic: %v", AddFaasRedisTopic)
	log.Printf("RemoveFaasRedisTopic: %v", RemoveFaasRedisTopic)

	r := gin.Default()

	rdb := redis.NewClient(&redis.Options{
		Addr: RedisAddrENV,
	})

	PushFAASCrement(r, rdb, AddFaasRedisTopic)
	PushFAASRemove(r, rdb, RemoveFaasRedisTopic)

	log.Printf("🚀 REST API running on http://localhost:%s", RestPortEnv)

	if err := r.Run(":" + RestPortEnv); err != nil {
		log.Fatalf("Failed to start REST server: %v", err)
	}
}

type PushFunctionParams struct {
	FunctionName string `json:"function-name" binding:"required"`
}

func PushFAASCrement(r *gin.Engine, client *redis.Client, AddFaasRedisTopic string) {
	r.POST("/pushFAASIncrement", func(c *gin.Context) {
		var input PushFunctionParams

		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		key := AddFaasRedisTopic + ":" + input.FunctionName
		channel := FAASTopic + "/" + input.FunctionName

		newVal, err := client.Incr(ctx, key).Result()
		if err != nil {
			fmt.Println("Error incrementing: ", err)
			c.JSON(501, gin.H{"Error incrementing: ": err})
		}

		err = client.Publish(ctx, channel, newVal).Err()
		if err != nil {
			fmt.Println("Error publishing: ", err)
			c.JSON(501, gin.H{"Error publishing: ": err})
		}

		fmt.Printf("Incremented '%s' to %d\n", input.FunctionName, newVal)

		c.JSON(200, gin.H{})
	})

	r.POST("/pushFAASDecrement", func(c *gin.Context) {
		var input PushFunctionParams

		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		key := AddFaasRedisTopic + ":" + input.FunctionName
		channel := FAASTopic + "/" + input.FunctionName

		newVal, err := client.Decr(ctx, key).Result()
		if err != nil {
			fmt.Println("Error incrementing: ", err)
			c.JSON(501, gin.H{"Error incrementing: ": err})
		}

		err = client.Publish(ctx, channel, newVal).Err()
		if err != nil {
			fmt.Println("Error publishing: ", err)
			c.JSON(501, gin.H{"Error publishing: ": err})
		}

		fmt.Printf("Decremented '%s' to %d\n", input.FunctionName, newVal)

		c.JSON(200, gin.H{})
	})
}

type PushFunctionRemoveParams struct {
	FunctionName string `json:"function-name" binding:"required"`
	FunctionPort int64  `json:"function-port" binding:"required"`
}

type FAASRemovePayload struct {
	Function string `json:"function"`
	Port     int64  `json:"port"`
}

func PushFAASRemove(r *gin.Engine, client *redis.Client, RemoveFaasRedisTopic string) {
	r.POST("/pushFAASRemove", func(c *gin.Context) {
		var input PushFunctionRemoveParams

		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		key := RemoveFaasRedisTopic + ":" + input.FunctionName
		channel := FAASTopic + "/" + input.FunctionName

		payload := FAASRemovePayload{
			Function: input.FunctionName,
			Port:     input.FunctionPort,
		}

		// 4. Convert struct to JSON bytes
		jsonBytes, err := json.Marshal(payload)
		if err != nil {
			fmt.Println("Error marshalling JSON:", err)
			return
		}

		// 5. Publish the JSON string
		// string(jsonBytes) converts the bytes to a string for Redis
		err = client.Publish(ctx, channel, string(jsonBytes)).Err()
		if err != nil {
			fmt.Println("Error publishing:", err)
		}

		fmt.Printf("📤 Published JSON: %s\n", string(jsonBytes))
		c.JSON(200, gin.H{"status": "ok", "new_value": key})
	})
}
