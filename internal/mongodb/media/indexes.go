package media

import (
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// indexes are the secondary indexes for the media collection.
//
// MongoDB is schemaless, so there is no field-by-field declaration to keep in
// sync with models.Media — adding a field to the model needs nothing here. What
// does belong here is an index for every field we filter or sort on, since
// without one those queries become collection scans.
//
//   - creator_id : FindByCreator filters on it.
//   - status     : the cleanup sweep and the pipeline both read by status.
//   - category   : backs navigation and filtering.
//   - tags       : a multikey index; MongoDB indexes each array element.
//   - created_at : descending, for newest-first listings.
//
// _id is indexed and unique automatically, so the media id needs no entry.
var indexes = []mongo.IndexModel{
	{Keys: bson.D{{Key: "creator_id", Value: 1}}, Options: options.Index().SetName("creator_id_1")},
	{Keys: bson.D{{Key: "status", Value: 1}}, Options: options.Index().SetName("status_1")},
	{Keys: bson.D{{Key: "category", Value: 1}}, Options: options.Index().SetName("category_1")},
	{Keys: bson.D{{Key: "tags", Value: 1}}, Options: options.Index().SetName("tags_1")},
	{Keys: bson.D{{Key: "created_at", Value: -1}}, Options: options.Index().SetName("created_at_-1")},
}
