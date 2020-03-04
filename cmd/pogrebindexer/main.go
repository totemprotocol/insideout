package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	stdlog "log"
	"os"
	"path"
	"time"

	"github.com/fxamacker/cbor"
	log "github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/golang/geo/s2"
	"github.com/namsral/flag"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/twpayne/go-geom/encoding/geojson"

	"github.com/akhenakh/insideout"
	"github.com/akhenakh/insideout/loglevel"
	spogreb "github.com/akhenakh/insideout/storage/pogreb"
)

/*
00 		85011012.19 	km2 	  	7842 km 	7842 km
01 		21252753.05 	km2 	  	3921 km 	5004 km
02 	 	5313188.26 		km2 	  	1825 km 	2489 km
03 		1328297.07 		km2 	  	1130 km 	1310 km
04  	332074.27 		km2 	  	579 km 	636 km
05 	 	83018.57 		km2 	  	287 km 	315 km
06 	    20754.64 		km2 	  	143 km 	156 km
07 		5188.66 		km2 	  	72 km 	78 km
08 	 	1297.17 		km2 	  	36 km 	39 km
09 	 	324.29 			km2 	  	18 km 	20 km
10 	 	81.07 			km2 	  	9 km 	10 km
11 	 	20.27 			km2 	  	4 km 	5 km
12 	 	5.07 			km2 	  	2 km 	2 km
13 	 	1.27 			km2 	  	1123 m 	1225 m
14 		0.32 			km2 	  	562 m 	613 m
15 	 	79172.67 		m2 	  		281 m 	306 m
16 	 	19793.17 		m2 	  		140 m 	153 m
17 	 	4948.29 		m2 	  		70 m 	77 m
18 	 	1237.07 		m2 	  		35 m 	38 m
19 	 	309.27 			m2 	  		18 m 	19 m
20 	 	77.32 			m2 	  		9 m 	10 m
21 	 	19.33 			m2 	  		4 m 	5 m
22 	 	4.83 			m2 	  		2 m 	2 m
23 	 	1.21 			m2 	  		110 cm 	120 cm
24 		0.30 			m2 	  		55 cm 	60 cm
25 	 	755.05 			cm2 	  	27 cm 	30 cm
26 	 	188.76 			cm2 	  	14 cm 	15 cm
27 	 	47.19 			cm2 	  	7 cm 	7 cm
28 	 	11.80 			cm2 	  	3 cm 	4 cm
29 	 	2.95 			cm2 	  	17 mm 	18 mm
30 	 	0.74 			cm2 	  	8 mm 	9 mm
*/
const appName = "pogrebindexer"

var (
	version = "no version from LDFLAGS"

	logLevel             = flag.String("logLevel", "INFO", "DEBUG|INFO|WARN|ERROR")
	insideMaxLevelCover  = flag.Int("insideMaxLevelCover", 16, "Max s2 level for inside cover")
	insideMinLevelCover  = flag.Int("insideMinLevelCover", 10, "Min s2 level for inside cover")
	insideMaxCellsCover  = flag.Int("insideMaxCellsCover", 24, "Max s2 Cells count for inside cover")
	outsideMaxLevelCover = flag.Int("outsideMaxLevelCover", 15, "Max s2 level for outside cover")
	outsideMinLevelCover = flag.Int("outsideMinLevelCover", 10, "Min s2 level for outside cover")
	outsideMaxCellsCover = flag.Int("outsideMaxCellsCover", 16, "Max s2 Cells count for outside cover")
	warningCellsCover    = flag.Int("warningCellsCover", 1000, "warning limit cover count")

	filePath = flag.String("filePath", "", "FeatureCollection GeoJSON file to index")
	dbPath   = flag.String("dbPath", "inside.db", "Database path")
)

func main() {
	flag.Parse()

	logger := log.NewJSONLogger(log.NewSyncWriter(os.Stdout))
	logger = log.With(logger, "caller", log.Caller(5), "ts", log.DefaultTimestampUTC)
	logger = log.With(logger, "app", appName)
	logger = loglevel.NewLevelFilterFromString(logger, *logLevel)
	stdlog.SetOutput(log.NewStdlibAdapter(logger))

	level.Info(logger).Log("msg", "Starting app", "version", version)

	var fc geojson.FeatureCollection

	// reading GeoJSON
	file, err := os.Open(*filePath)
	if err != nil {
		level.Error(logger).Log("msg", "failed to open GeoJSON", "error", err, "file_path", *filePath)
		os.Exit(2)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	err = decoder.Decode(&fc)
	if err != nil {
		level.Error(logger).Log("msg", "failed to decode GeoJSON", "error", err, "file_path", *filePath)
		os.Exit(2)
	}

	storage, clean, err := spogreb.NewStorage(*dbPath, logger)
	if err != nil {
		level.Error(logger).Log("msg", "failed to open storage", "error", err, "db_path", *dbPath)
		os.Exit(2)
	}
	defer clean()

	icoverer := &s2.RegionCoverer{
		MinLevel: *insideMinLevelCover,
		MaxLevel: *insideMaxLevelCover,
		MaxCells: *insideMaxCellsCover,
	}
	ocoverer := &s2.RegionCoverer{
		MinLevel: *outsideMinLevelCover,
		MaxLevel: *outsideMaxLevelCover,
		MaxCells: *outsideMaxCellsCover,
	}

	var count uint32

	for _, f := range fc.Features {
		// cover inside
		cui, err := insideout.GeoJSONCoverCellUnion(f, icoverer, true)
		if err != nil {
			level.Warn(logger).Log("msg", "error covering inside", "error", err, "feature_properties", f.Properties)
			continue
		}

		// cover outside
		cuo, err := insideout.GeoJSONCoverCellUnion(f, ocoverer, false)
		if err != nil {
			level.Warn(logger).Log("msg", "error covering outside", "error", err, "feature_properties", f.Properties)
			continue
		}

		// store interior cover
		for fi, cu := range cui {
			if *warningCellsCover != 0 && len(cu) > *warningCellsCover {
				level.Warn(logger).Log(
					"msg", fmt.Sprintf("inside cover too big%d polygon #%d %s", len(cui), fi, f.Properties),
					"error", err, "feature_properties", f.Properties,
				)

				continue
			}
			for _, c := range cu {
				// value is the feature id: current count, the polygon index in a multipolygon: fi
				v := make([]byte, 6)
				binary.BigEndian.PutUint32(v, count)
				binary.BigEndian.PutUint16(v[4:], uint16(fi))
				// append to existing if any
				ev, err := storage.Get(insideout.InsideKey(c))
				if err != nil {
					if err != leveldb.ErrNotFound {
						level.Error(logger).Log("msg", "failed reading DB", "error", err, "db_path", *dbPath)
						os.Exit(2)
					}
				}
				v = append(v, ev...)
				err = storage.Put(insideout.InsideKey(c), v)
				if err != nil {
					level.Error(logger).Log("msg", "failed storing DB", "error", err, "db_path", *dbPath)
					os.Exit(2)
				}
			}
		}

		// store outside cover
		for fi, cu := range cuo {
			if *warningCellsCover != 0 && len(cu) > *warningCellsCover {
				level.Warn(logger).Log(
					"msg", fmt.Sprintf("outisde cover too big%d polygon #%d %s", len(cui), fi, f.Properties),
					"error", err, "feature_properties", f.Properties,
				)
				continue
			}
			for _, c := range cu {
				// TODO: filter cells already indexed by inside cover

				// value is the feature id: current count, the polygon index in a multipolygon: fi
				v := make([]byte, 6)
				binary.BigEndian.PutUint32(v, count)
				binary.BigEndian.PutUint16(v[4:], uint16(fi))
				// append to existing if any
				ev, err := storage.Get(insideout.OutsideKey(c))
				if err != nil {
					if err != leveldb.ErrNotFound {
						level.Error(logger).Log("msg", "failed reading DB", "error", err, "db_path", *dbPath)
						os.Exit(2)
					}
				}
				v = append(v, ev...)
				err = storage.Put(insideout.OutsideKey(c), v)
				if err != nil {
					level.Error(logger).Log("msg", "failed storing DB", "error", err, "db_path", *dbPath)
					os.Exit(2)
				}
			}
		}
		lb, err := insideout.GeoJSONEncodeLoops(f)
		if err != nil {
			level.Error(logger).Log("msg", "can't encode loop", "error", err)
			os.Exit(2)
		}

		// store feature
		b := new(bytes.Buffer)
		enc := cbor.NewEncoder(b, cbor.CanonicalEncOptions())

		// TODO: filter cuo cui[fi].ContainsCellID(c)
		fs := &insideout.FeatureStorage{Properties: f.Properties, LoopsBytes: lb}
		if err := enc.Encode(fs); err != nil {
			level.Error(logger).Log("msg", "can't encode FeatureStorage", "error", err)
			os.Exit(2)
		}

		err = storage.Put(insideout.FeatureKey(count), b.Bytes())
		if err != nil {
			level.Error(logger).Log("msg", "failed storing DB", "error", err, "db_path", *dbPath)
			os.Exit(2)
		}
		// store cells for tree
		b = new(bytes.Buffer)
		enc = cbor.NewEncoder(b, cbor.CanonicalEncOptions())
		cs := &insideout.CellsStorage{
			CellsIn:  cui,
			CellsOut: cuo,
		}

		if err := enc.Encode(cs); err != nil {
			level.Error(logger).Log("msg", "can't encode CellsStorage", "error", err)
			os.Exit(2)
		}

		err = storage.Put(insideout.CellKey(count), b.Bytes())
		if err != nil {
			level.Error(logger).Log("msg", "failed storing DB", "error", err, "db_path", *dbPath)
			os.Exit(2)
		}
		level.Debug(logger).Log(
			"msg", "stored FeatureStorage",
			"feature_properties", f.Properties,
			"loop_count", len(fs.LoopsBytes),
			"inside_loop_id", count,
		)

		//log.Println(f.Properties, len(cui), len(cuo))

		count++
	}

	infoBytes := new(bytes.Buffer)

	// Finding the lowest cover level
	minCoverLevel := *outsideMinLevelCover
	if *insideMinLevelCover < *outsideMinLevelCover {
		minCoverLevel = *insideMinLevelCover
	}
	infos := &insideout.IndexInfos{
		Filename:       path.Base(*filePath),
		IndexTime:      time.Now(),
		IndexerVersion: version,
		FeatureCount:   count,
		MinCoverLevel:  minCoverLevel,
	}

	enc := cbor.NewEncoder(infoBytes, cbor.CanonicalEncOptions())
	if err := enc.Encode(infos); err != nil {
		level.Error(logger).Log("msg", "failed encoding IndexInfos", "error", err)
		os.Exit(2)
	}

	if err := storage.Put(insideout.InfoKey(), infoBytes.Bytes()); err != nil {
		level.Error(logger).Log("msg", "failed writing IndexInfos to DB", "error", err, "db_path", *dbPath)
		os.Exit(2)
	}

	level.Info(logger).Log("msg", "stored index_infos", "feature_count", infos.FeatureCount)
}
