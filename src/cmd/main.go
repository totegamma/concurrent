package main

import (
	"fmt"
	"net/http"

	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/totegamma/concurrent/x/association"
	"github.com/totegamma/concurrent/x/character"
	"github.com/totegamma/concurrent/x/message"
	"github.com/totegamma/concurrent/x/socket"
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
    db.AutoMigrate(&character.Character{}, &association.Association{}, &message.Message{})

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

    socketService := socket.NewSocketService();

    socketHandler := SetupSocketHandler(socketService)
    messageHandler := SetupMessageHandler(db, rdb, socketService)
    characterHandler := SetupCharacterHandler(db)
    associationHandler := SetupAssociationHandler(db, rdb, socketService)
    streamHandler := SetupStreamHandler(rdb)
    webfingerHandler := SetupWebfingerHandler(db)
    activityPubHandler := SetupActivityPubHandler(db)

    fmt.Println("start web")
    http.HandleFunc("/messages", messageHandler.Handle)
    http.HandleFunc("/messages/", messageHandler.Handle)
    http.HandleFunc("/characters", characterHandler.Handle)
    http.HandleFunc("/associations", associationHandler.Handle)
    http.HandleFunc("/stream", streamHandler.Handle)
    http.HandleFunc("/stream/list", streamHandler.HandleList)
    http.HandleFunc("/socket", socketHandler.Handle)
    http.HandleFunc("/.well-known/webfinger", webfingerHandler.Handle)
    http.Handle("/ap/", http.StripPrefix("/ap", http.HandlerFunc(activityPubHandler.Handle)))
    http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprint(w, "ok");
    })
    http.ListenAndServe(":8000", nil)
}

