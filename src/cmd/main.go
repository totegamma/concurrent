package main

import (
    "fmt"
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
)

func main() {

    dsn := "host=localhost user=postgres password=postgres dbname=concurrent port=5432 sslmode=disable"
    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        fmt.Println("failed to connect database");
        panic("failed to connect database")
    }

    // Migrate the schema
    fmt.Println("start migrate")
    db.AutoMigrate(&character.Character{}, &association.Association{}, &message.Message{}, &stream.Stream{}, &host.Host{})

    var count int64
    db.Table("information_schema.triggers").Where("trigger_name = 'attach_association_trigger'").Count(&count)
    if count == 0 {
        fmt.Println("Create attach_association_trigger")
        attach_association_hook := `
            CREATE FUNCTION attach_association() RETURNS TRIGGER AS $attach_association$
            BEGIN
                UPDATE messages
                SET associations = ARRAY_APPEND(associations, NEW.id)
                WHERE id = NEW.target;
                return NEW;
            END;
            $attach_association$
            LANGUAGE plpgsql;
            CREATE TRIGGER attach_association_trigger
                AFTER INSERT
                ON associations
                FOR EACH ROW EXECUTE FUNCTION attach_association();
        `
        db.Exec(attach_association_hook)
    }

    db.Table("information_schema.triggers").Where("trigger_name = 'detach_association_trigger'").Count(&count)
    if count == 0 {
        fmt.Println("Create detach_association_trigger")
        detach_association_hook := `
            CREATE FUNCTION detach_association() RETURNS TRIGGER AS $detach_association$
            BEGIN
                UPDATE messages
                SET associations = ARRAY_REMOVE(associations, OLD.id)
                WHERE id = OLD.target;
                return OLD;
            END;
            $detach_association$
            LANGUAGE plpgsql;
            CREATE TRIGGER detach_association_trigger
                BEFORE DELETE 
                ON associations
                FOR EACH ROW EXECUTE FUNCTION detach_association();
        `
        db.Exec(detach_association_hook)
    }
    fmt.Println("done!")

    rdb := redis.NewClient(&redis.Options{
        Addr:     "localhost:6379",
        Password: "", // no password set
        DB:       0,  // use default DB
    })

    socketService := socket.NewService();

    socketHandler := SetupSocketHandler(socketService)
    messageHandler := SetupMessageHandler(db, rdb, socketService)
    characterHandler := SetupCharacterHandler(db)
    associationHandler := SetupAssociationHandler(db, rdb, socketService)
    streamHandler := SetupStreamHandler(db, rdb)
    hostHandler := SetupHostHandler(db)

    e := echo.New()
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
    e.GET("/host/:id", hostHandler.Get)
    e.PUT("/host", hostHandler.Upsert)
    e.GET("/host", hostHandler.List)
    e.GET("/health", func(c echo.Context) (err error) {
        return c.String(http.StatusOK, "ok")
    })

    e.Logger.Fatal(e.Start(":8000"))
}

