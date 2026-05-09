package datastore

import (
	"context"
	"strconv"
	"strings"
	"time"

	gcdatastore "cloud.google.com/go/datastore"
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
	client    *gcdatastore.Client
	namespace string
}

func NewClient(ctx context.Context, projectID string) (*gcdatastore.Client, error) {
	return gcdatastore.NewClient(ctx, projectID)
}

func newStore(client *gcdatastore.Client, namespace string) *store {
	return &store{client: client, namespace: namespace}
}

func (s *store) key(kind, name string) *gcdatastore.Key {
	key := gcdatastore.NameKey(kind, name, nil)
	key.Namespace = s.namespace
	return key
}

func (s *store) incompleteKey(kind string) *gcdatastore.Key {
	key := gcdatastore.IncompleteKey(kind, nil)
	key.Namespace = s.namespace
	return key
}

func (s *store) query(kind string) *gcdatastore.Query {
	query := gcdatastore.NewQuery(kind)
	if s.namespace != "" {
		query = query.Namespace(s.namespace)
	}
	return query
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
	ID          string    `datastore:"id,noindex"`
	IP          string    `datastore:"ip,noindex"`
	Document    string    `datastore:"document,noindex"`
	Proof       string    `datastore:"proof,noindex"`
	GcCandidate bool      `datastore:"gcCandidate"`
	CDate       time.Time `datastore:"cdate"`
}

type commitOwnerModel struct {
	Owner           string    `datastore:"owner"`
	CommitID        string    `datastore:"commitID"`
	CommitCreatedAt time.Time `datastore:"commitCreatedAt"`
}

type recordModel struct {
	DocumentID    string    `datastore:"documentID,noindex"`
	Owner         string    `datastore:"owner"`
	Redirect      string    `datastore:"redirect,noindex"`
	Schema        string    `datastore:"schema"`
	Policies      string    `datastore:"policies,noindex"`
	Distributions []string  `datastore:"distributions,noindex"`
	CreatedAt     time.Time `datastore:"createdAt"`
	CDate         time.Time `datastore:"cdate"`
}

type recordKeyModel struct {
	URI             string    `datastore:"uri,noindex"`
	ParentURI       string    `datastore:"parentURI"`
	RecordID        string    `datastore:"recordID"`
	RecordOwner     string    `datastore:"recordOwner"`
	RecordSchema    string    `datastore:"recordSchema"`
	RecordCreatedAt time.Time `datastore:"recordCreatedAt"`
	Redirect        string    `datastore:"redirect,noindex"`
	Prefixes        []string  `datastore:"prefixes"`
}

type associationModel struct {
	TargetURI  string    `datastore:"targetURI"`
	DocumentID string    `datastore:"documentID"`
	Owner      string    `datastore:"owner"`
	Author     string    `datastore:"author"`
	Schema     string    `datastore:"schema"`
	Variant    string    `datastore:"variant"`
	Unique     string    `datastore:"unique,noindex"`
	CreatedAt  time.Time `datastore:"createdAt"`
	CDate      time.Time `datastore:"cdate"`
}

type ackModel struct {
	From       string    `datastore:"from"`
	To         string    `datastore:"to"`
	Context    string    `datastore:"context"`
	DocumentID string    `datastore:"documentID"`
	Valid      bool      `datastore:"valid"`
	CreatedAt  time.Time `datastore:"createdAt"`
	CDate      time.Time `datastore:"cdate"`
}

type serverModel struct {
	ID          string    `datastore:"id,noindex"`
	CSID        string    `datastore:"csid"`
	Tag         string    `datastore:"tag,noindex"`
	Layer       string    `datastore:"layer,noindex"`
	WellKnown   string    `datastore:"wellKnown,noindex"`
	CDate       time.Time `datastore:"cdate"`
	MDate       time.Time `datastore:"mdate"`
	LastScraped time.Time `datastore:"lastScraped"`
}

type entityModel struct {
	ID         string    `datastore:"id,noindex"`
	Alias      string    `datastore:"alias"`
	Domain     string    `datastore:"domain"`
	Tag        string    `datastore:"tag,noindex"`
	DocumentID string    `datastore:"documentID"`
	CDate      time.Time `datastore:"cdate"`
	MDate      time.Time `datastore:"mdate"`
}

type entityMetaModel struct {
	ID      string    `datastore:"id,noindex"`
	Inviter string    `datastore:"inviter,noindex"`
	Info    string    `datastore:"info,noindex"`
	CDate   time.Time `datastore:"cdate"`
	MDate   time.Time `datastore:"mdate"`
}

type subscriptionModel struct {
	VendorID     string    `datastore:"vendorID"`
	Owner        string    `datastore:"owner"`
	Schemas      []string  `datastore:"schemas,noindex"`
	Prefixes     []string  `datastore:"prefixes,noindex"`
	Subscription string    `datastore:"subscription,noindex"`
	CDate        time.Time `datastore:"cdate"`
	MDate        time.Time `datastore:"mdate"`
}

type abuseReportModel struct {
	IP        string    `datastore:"ip,noindex"`
	Reporter  string    `datastore:"reporter"`
	TargetURI string    `datastore:"targetURI"`
	Body      string    `datastore:"body,noindex"`
	CDate     time.Time `datastore:"cdate"`
}
