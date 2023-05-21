package main

import (
    "fmt"
    "log"
    "net/http"

    "gorm.io/gorm"
    "gorm.io/driver/postgres"
    "github.com/redis/go-redis/v9"

    "github.com/labstack/echo/v4"
    "github.com/labstack/echo/v4/middleware"

    "github.com/totegamma/concurrent/x/association"
    "github.com/totegamma/concurrent/x/character"
    "github.com/totegamma/concurrent/x/message"
    "github.com/totegamma/concurrent/x/socket"
    "github.com/totegamma/concurrent/x/stream"
    "github.com/totegamma/concurrent/x/host"
    "github.com/totegamma/concurrent/x/util"
)

func main() {

    fmt.Print(concurrentBanner)

    e := echo.New()

    config := util.Config{}
    err := config.Load("/etc/concurrent/config.yaml")
    if err != nil {
        e.Logger.Fatal(err)
    }

    log.Print("Config loaded! I am: ", config.CCAddr)

    db, err := gorm.Open(postgres.Open(config.Dsn), &gorm.Config{})
    if err != nil {
        log.Println("failed to connect database");
        panic("failed to connect database")
    }

    // Migrate the schema
    log.Println("start migrate")
    db.AutoMigrate(&character.Character{},&message.Message{}, &association.Association{},  &stream.Stream{}, &host.Host{})

    rdb := redis.NewClient(&redis.Options{
        Addr:     config.RedisAddr,
        Password: "", // no password set
        DB:       0,  // use default DB
    })

    socketService := socket.NewService();

    socketHandler := SetupSocketHandler(socketService)
    messageHandler := SetupMessageHandler(db, rdb, socketService)
    characterHandler := SetupCharacterHandler(db)
    associationHandler := SetupAssociationHandler(db, rdb, socketService)
    streamHandler := SetupStreamHandler(db, rdb)
    hostHandler := SetupHostHandler(db, config)

    e.HideBanner = true
    e.Use(middleware.CORS())
    e.Use(middleware.Logger())
    e.Use(middleware.Recover())

    e.GET("/messages/:id", messageHandler.Get)
    e.POST("/messages", messageHandler.Post)
    e.GET("/characters", characterHandler.Get)
    e.PUT("/characters", characterHandler.Put)
    e.GET("/associations/:id", associationHandler.Get)
    e.POST("/associations", associationHandler.Post)
    e.DELETE("/associations", associationHandler.Delete)
    e.GET("/stream", streamHandler.Get)
    e.POST("/stream", streamHandler.Post)
    e.PUT("/stream", streamHandler.Put)
    e.GET("/stream/recent", streamHandler.Recent)
    e.GET("/stream/list", streamHandler.List)
    e.GET("/stream/range", streamHandler.Range)
    e.GET("/socket", socketHandler.Connect)
    e.GET("/host/:id", hostHandler.Get) //TODO deprecated. remove later
    e.PUT("/host", hostHandler.Upsert)
    e.GET("/host", hostHandler.Profile)
    e.GET("/host/list", hostHandler.List)
    e.POST("/host/hello", hostHandler.Hello)
    e.GET("/health", func(c echo.Context) (err error) {
        return c.String(http.StatusOK, "ok")
    })

    e.Logger.Fatal(e.Start(":8000"))
}

