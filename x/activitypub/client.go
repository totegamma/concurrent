package activitypub

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"github.com/go-fed/httpsig"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"io/ioutil"
	"net/http"
	"time"
)

// FetchPerson fetches a person from remote ap server.
func FetchPerson(ctx context.Context, actor string) (Person, error) {
	_, span := tracer.Start(ctx, "FetchPerson")
	defer span.End()

	var person Person
	req, err := http.NewRequest("GET", actor, nil)
	if err != nil {
		return person, err
	}
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))
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

// PostToInbox posts a message to remote ap server.
func (h Handler) PostToInbox(ctx context.Context, inbox string, object interface{}, signUser string) error {

	objectBytes, err := json.Marshal(object)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", inbox, bytes.NewBuffer(objectBytes))
	if err != nil {
		return err
	}
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))
	req.Header.Set("Content-Type", "application/activity+json")
	req.Header.Set("Date", time.Now().UTC().Format(http.TimeFormat))
	client := new(http.Client)

	entity, err := h.repo.GetEntityByID(ctx, signUser)
	if err != nil {
		return err
	}

	block, _ := pem.Decode([]byte(entity.Privatekey))
	if block == nil {
		return fmt.Errorf("failed to parse PEM block containing the key")
	}

	// parse ed25519 private key
	priv, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse DER encoded private key: " + err.Error())
	}

	prefs := []httpsig.Algorithm{httpsig.ED25519}
	digestAlgorithm := httpsig.DigestSha256
	headersToSign := []string{httpsig.RequestTarget, "date", "digest"}
	signer, _, err := httpsig.NewSigner(prefs, digestAlgorithm, headersToSign, httpsig.Signature, 0)
	if err != nil {
		return err
	}
	err = signer.SignRequest(priv, "https://"+h.config.Concurrent.FQDN+"/ap/key/"+signUser, req, objectBytes)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}
