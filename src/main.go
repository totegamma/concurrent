package main

import (
    "fmt"
    "net/http"
    "gorm.io/gorm"
    "gorm.io/driver/postgres"
    "concurrent/domain/model"
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
    db.AutoMigrate(&model.Character{}, &model.Association{}, &model.Message{})

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
                WHERE id = NEW.target;
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

    concurrentApp := SetupConcurrentApp(db)

    fmt.Println("start web")
    http.HandleFunc("/", concurrentApp.ServeHTTP)
    http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprint(w, "ok");
    })
    http.ListenAndServe(":8000", nil)
}

