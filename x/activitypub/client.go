package activitypub

import (
    "net/http"
    "io/ioutil"
    "encoding/json"
)

// FetchPerson fetches a person from remote ap server.
func FetchPerson(actor string) (Person, error) {
    var person Person
    req, err := http.NewRequest("GET", actor, nil)
    if err != nil {
        return person, err
    }
    req.Header.Set("Accept", "application/activity+json")
    client := new(http.Client)
    resp, err := client.Do(req)
    if err != nil {
        return person, err
    }
    defer resp.Body.Close()

    body, _ := ioutil.ReadAll(resp.Body)

    err = json.Unmarshal(body, &person)
    if err != nil {
        return person, err
    }

    return person, nil
}

