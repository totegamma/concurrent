package firestore

import (
	"context"
	"encoding/base64"
	"strconv"
	"strings"
	"time"

	gcfirestore "cloud.google.com/go/firestore"
)

const (
	kindCommitLog    = "CommitLog"
	kindCommitOwner  = "CommitOwner"
	kindRecord       = "Record"
	kindRecordKey    = "RecordKey"
	kindAssociation  = "Association"
	kindAck          = "Ack"
	kindServer       = "Server"
	kindEntity       = "Entity"
	kindEntityMeta   = "EntityMeta"
	kindSubscription = "Subscription"
	kindAbuseReport  = "AbuseReport"
)

type store struct {
	client    *gcfirestore.Client
	namespace string
}

func NewClient(ctx context.Context, projectID, databaseID string) (*gcfirestore.Client, error) {
	if databaseID != "" {
		return gcfirestore.NewClientWithDatabase(ctx, projectID, databaseID)
	}
	return gcfirestore.NewClient(ctx, projectID)
}

func newStore(client *gcfirestore.Client, namespace string) *store {
	return &store{client: client, namespace: namespace}
}

func (s *store) collection(kind string) *gcfirestore.CollectionRef {
	if s.namespace == "" {
		return s.client.Collection(kind)
	}
	return s.client.Collection("Namespaces").Doc(documentID(s.namespace)).Collection(kind)
}

func (s *store) doc(kind, name string) *gcfirestore.DocumentRef {
	return s.collection(kind).Doc(documentID(name))
}

func (s *store) newDoc(kind string) *gcfirestore.DocumentRef {
	return s.collection(kind).NewDoc()
}

func (s *store) query(kind string) gcfirestore.Query {
	return s.collection(kind).Query
}

func documentID(name string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(name))
}

func compoundKey(parts ...string) string {
	var builder strings.Builder
	for _, part := range parts {
		builder.WriteString(strconv.Itoa(len(part)))
		builder.WriteByte(':')
		builder.WriteString(part)
		builder.WriteByte('|')
	}
	return builder.String()
}

type commitLogModel struct {
	ID          string    `firestore:"id"`
	IP          string    `firestore:"ip"`
	Document    string    `firestore:"document"`
	Proof       string    `firestore:"proof"`
	GcCandidate bool      `firestore:"gcCandidate"`
	CDate       time.Time `firestore:"cdate"`
}

type commitOwnerModel struct {
	Owner           string    `firestore:"owner"`
	CommitID        string    `firestore:"commitID"`
	CommitCreatedAt time.Time `firestore:"commitCreatedAt"`
}

type recordModel struct {
	DocumentID    string    `firestore:"documentID"`
	Owner         string    `firestore:"owner"`
	Redirect      string    `firestore:"redirect"`
	Schema        string    `firestore:"schema"`
	Policies      string    `firestore:"policies"`
	Distributions []string  `firestore:"distributions"`
	CreatedAt     time.Time `firestore:"createdAt"`
	CDate         time.Time `firestore:"cdate"`
}

type recordKeyModel struct {
	URI             string    `firestore:"uri"`
	ParentURI       string    `firestore:"parentURI"`
	RecordID        string    `firestore:"recordID"`
	RecordOwner     string    `firestore:"recordOwner"`
	RecordSchema    string    `firestore:"recordSchema"`
	RecordCreatedAt time.Time `firestore:"recordCreatedAt"`
	Redirect        string    `firestore:"redirect"`
	Prefixes        []string  `firestore:"prefixes"`
}

type associationModel struct {
	TargetURI  string    `firestore:"targetURI"`
	DocumentID string    `firestore:"documentID"`
	Owner      string    `firestore:"owner"`
	Author     string    `firestore:"author"`
	Schema     string    `firestore:"schema"`
	Variant    string    `firestore:"variant"`
	Unique     string    `firestore:"unique"`
	CreatedAt  time.Time `firestore:"createdAt"`
	CDate      time.Time `firestore:"cdate"`
}

type ackModel struct {
	From       string    `firestore:"from"`
	To         string    `firestore:"to"`
	Context    string    `firestore:"context"`
	DocumentID string    `firestore:"documentID"`
	Valid      bool      `firestore:"valid"`
	CreatedAt  time.Time `firestore:"createdAt"`
	CDate      time.Time `firestore:"cdate"`
}

type serverModel struct {
	ID          string    `firestore:"id"`
	CSID        string    `firestore:"csid"`
	Tag         string    `firestore:"tag"`
	Layer       string    `firestore:"layer"`
	WellKnown   string    `firestore:"wellKnown"`
	CDate       time.Time `firestore:"cdate"`
	MDate       time.Time `firestore:"mdate"`
	LastScraped time.Time `firestore:"lastScraped"`
}

type entityModel struct {
	ID         string    `firestore:"id"`
	Alias      string    `firestore:"alias"`
	Domain     string    `firestore:"domain"`
	Tag        string    `firestore:"tag"`
	DocumentID string    `firestore:"documentID"`
	CDate      time.Time `firestore:"cdate"`
	MDate      time.Time `firestore:"mdate"`
}

type entityMetaModel struct {
	ID      string    `firestore:"id"`
	Inviter string    `firestore:"inviter"`
	Info    string    `firestore:"info"`
	CDate   time.Time `firestore:"cdate"`
	MDate   time.Time `firestore:"mdate"`
}

type subscriptionModel struct {
	VendorID     string    `firestore:"vendorID"`
	Owner        string    `firestore:"owner"`
	Schemas      []string  `firestore:"schemas"`
	Prefixes     []string  `firestore:"prefixes"`
	Subscription string    `firestore:"subscription"`
	CDate        time.Time `firestore:"cdate"`
	MDate        time.Time `firestore:"mdate"`
}

type abuseReportModel struct {
	IP        string    `firestore:"ip"`
	Reporter  string    `firestore:"reporter"`
	TargetURI string    `firestore:"targetURI"`
	Body      string    `firestore:"body"`
	CDate     time.Time `firestore:"cdate"`
}
