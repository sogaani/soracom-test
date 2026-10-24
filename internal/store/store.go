// Package store persists messages received from SORACOM Beam in MongoDB.
package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Meta identifies where a message came from. It is the time series
// collection's metaField, so queries filtering on it are efficient.
type Meta struct {
	IMSI   string `bson:"imsi,omitempty" json:"imsi,omitempty"`
	IMEI   string `bson:"imei,omitempty" json:"imei,omitempty"`
	SIMID  string `bson:"simId,omitempty" json:"simId,omitempty"`
	MSISDN string `bson:"msisdn,omitempty" json:"msisdn,omitempty"`
	// Source is the protocol the device used: http, tcp, udp, sms or ussd.
	Source string `bson:"source,omitempty" json:"source,omitempty"`
}

// Message is one payload forwarded by Beam.
type Message struct {
	Time        time.Time `bson:"time" json:"time"`
	Meta        Meta      `bson:"meta" json:"meta"`
	ContentType string    `bson:"contentType,omitempty" json:"contentType,omitempty"`
	// Data holds the payload when it is a JSON object (or key=value text),
	// so its fields can be graphed directly.
	Data map[string]any `bson:"data,omitempty" json:"data,omitempty"`
	// Text holds a non-JSON text payload.
	Text string `bson:"text,omitempty" json:"text,omitempty"`
	// Raw holds a binary payload.
	Raw []byte `bson:"raw,omitempty" json:"raw,omitempty"`
}

// Query selects messages in [From, To). When more than Limit match, the
// newest Limit are returned. Results are in ascending time order.
type Query struct {
	From, To time.Time
	IMSI     string // optional
	Source   string // optional
	Limit    int64
}

// Store is a MongoDB backed message store.
type Store struct {
	client *mongo.Client
	coll   *mongo.Collection
}

// Open connects to MongoDB and makes sure the collection exists.
func Open(ctx context.Context, uri, database, collection string) (*Store, error) {
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}
	if err := client.Ping(ctx, nil); err != nil {
		return nil, errors.Join(fmt.Errorf("ping mongodb: %w", err), client.Disconnect(ctx))
	}
	s := &Store{client: client, coll: client.Database(database).Collection(collection)}
	if err := s.ensureCollection(ctx); err != nil {
		return nil, errors.Join(err, client.Disconnect(ctx))
	}
	return s, nil
}

// ensureCollection creates a time series collection on first start. An
// existing collection is left as it is.
func (s *Store) ensureCollection(ctx context.Context) error {
	db := s.coll.Database()
	names, err := db.ListCollectionNames(ctx, bson.D{{Key: "name", Value: s.coll.Name()}})
	if err != nil {
		return err
	}
	if len(names) > 0 {
		return nil
	}
	ts := options.TimeSeries().SetTimeField("time").SetMetaField("meta").SetGranularity("seconds")
	err = db.CreateCollection(ctx, s.coll.Name(), options.CreateCollection().SetTimeSeriesOptions(ts))
	// Another replica may have created it concurrently.
	var cmdErr mongo.CommandError
	if errors.As(err, &cmdErr) && cmdErr.Name == "NamespaceExists" {
		return nil
	}
	return err
}

// Close disconnects from MongoDB.
func (s *Store) Close(ctx context.Context) error {
	return s.client.Disconnect(ctx)
}

// Ping checks the connection, for readiness probes.
func (s *Store) Ping(ctx context.Context) error {
	return s.client.Ping(ctx, nil)
}

// Insert stores a message.
func (s *Store) Insert(ctx context.Context, m *Message) error {
	_, err := s.coll.InsertOne(ctx, m)
	return err
}

// Find returns messages matching q.
func (s *Store) Find(ctx context.Context, q Query) (msgs []Message, err error) {
	filter := bson.D{{Key: "time", Value: bson.D{{Key: "$gte", Value: q.From}, {Key: "$lt", Value: q.To}}}}
	if q.IMSI != "" {
		filter = append(filter, bson.E{Key: "meta.imsi", Value: q.IMSI})
	}
	if q.Source != "" {
		filter = append(filter, bson.E{Key: "meta.source", Value: q.Source})
	}
	opts := options.Find().SetSort(bson.D{{Key: "time", Value: -1}}).SetLimit(q.Limit).
		SetProjection(bson.D{{Key: "_id", Value: 0}})
	cur, err := s.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, cur.Close(ctx)) }()
	msgs = []Message{}
	if err := cur.All(ctx, &msgs); err != nil {
		return nil, err
	}
	slices.Reverse(msgs)
	return msgs, nil
}

// Devices returns the distinct IMSIs seen, for a Grafana variable.
func (s *Store) Devices(ctx context.Context) ([]string, error) {
	res := s.coll.Distinct(ctx, "meta.imsi", bson.D{})
	var imsis []string
	if err := res.Decode(&imsis); err != nil {
		return nil, err
	}
	if imsis == nil {
		imsis = []string{}
	}
	return imsis, nil
}
