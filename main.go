package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

// 配置結構
type Config struct {
	WeatherAPIKey string
	RedisHost     string
	RedisPort     string
	RedisPassword string
}

// 天氣服務結構
type WeatherService struct {
	config      Config
	redisClient *redis.Client
}

func main() {
	// 加載環境變數
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	config := Config{
		WeatherAPIKey: os.Getenv("WEATHER_API_KEY"),
		RedisHost:     os.Getenv("REDIS_HOST"),
		RedisPort:     os.Getenv("REDIS_PORT"),
		RedisPassword: os.Getenv("REDIS_PASSWORD"),
	}

	// 初始化 Redis 客戶端
	redisClient := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%s", config.RedisHost, config.RedisPort),
		Password: config.RedisPassword,
		DB:       0, // 默认 DB
	})

	// 驗證 Redis 連線
	ctx := context.Background()
	_, err = redisClient.Ping(ctx).Result()
	if err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}

	service := &WeatherService{
		config:      config,
		redisClient: redisClient,
	}

	// 初始化 Fiber
	app := fiber.New()

	// 定義路由
	app.Get("/weather/:city", service.getWeather)

	// 啟動服務器
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	log.Fatal(app.Listen(":" + port))
}

func (s *WeatherService) getWeather(c *fiber.Ctx) error {
	city := c.Params("city")
	cacheKey := fmt.Sprintf("weather:%s", strings.ToLower(city))
	ctx := context.Background()

	// 檢查 Redis 快取
	cachedData, err := s.redisClient.Get(ctx, cacheKey).Result()
	if err == nil {
		log.Println("Serving from cache")
		var weatherData interface{}
		json.Unmarshal([]byte(cachedData), &weatherData)
		return c.JSON(weatherData)
	}
	if err != redis.Nil {
		log.Printf("Redis error: %v", err)
	}

	// 調用 Visual Crossing API
	url := fmt.Sprintf("https://weather.visualcrossing.com/VisualCrossingWebServices/rest/services/timeline/%s?key=%s", city, s.config.WeatherAPIKey)
	resp, err := http.Get(url)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to fetch weather data"})
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.Status(resp.StatusCode).JSON(fiber.Map{"error": "Invalid city or API error"})
	}

	// 解析 API 響應
	var weatherData interface{}
	err = json.NewDecoder(resp.Body).Decode(&weatherData)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to parse weather data"})
	}

	// 存入 Redis，12 小時過期
	weatherJSON, err := json.Marshal(weatherData)
	if err != nil {
		log.Printf("Failed to marshal weather data: %v", err)
	} else {
		err = s.redisClient.SetEx(ctx, cacheKey, string(weatherJSON), time.Duration(12*60*60)*time.Second).Err()
		if err != nil {
			log.Printf("Failed to cache weather data: %v", err)
		}
	}

	log.Println("Serving from API")
	return c.JSON(weatherData)
}
