package repository

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/shenwei/inkstone/backend/internal/model"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func NewDB() *gorm.DB {
	host := getEnv("DB_HOST", "localhost")
	port := getEnv("DB_PORT", "5432")
	user := getEnv("DB_USER", "blog")
	pass := getEnv("DB_PASSWORD", "blog_dev_password")
	name := getEnv("DB_NAME", "blog_platform")

	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable TimeZone=Asia/Shanghai",
		host, port, user, pass, name)

	logLevel := gormlogger.Warn
	if getEnv("APP_ENV", "development") == "production" {
		logLevel = gormlogger.Error
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger:         gormlogger.Default.LogMode(logLevel),
		TranslateError: true,
	})
	if err != nil {
		log.Fatalf("failed to connect database: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("failed to get underlying sql.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)

	if err := db.AutoMigrate(
		&model.User{},
		&model.Article{},
		&model.ArticleRevision{},
		&model.Category{},
		&model.Tag{},
		&model.Comment{},
		&model.Reaction{},
		&model.Setting{},
		&model.Page{},
		&model.FriendLink{},
		&model.FileAsset{},
		&model.DailyStat{},
		&model.OperationLog{},
		&model.VisitorDay{},
		&model.FriendLinkApplication{},
		// POW 挑战必须持久化：跨请求存活，多副本部署下
		// 任意实例要能消费别的实例签发的挑战。
		&model.PowChallenge{},
	); err != nil {
		log.Fatalf("failed to migrate database: %v", err)
	}

	return db
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
