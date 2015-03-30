package main

import (
	"bytes"
	"encoding/gob"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/boltdb/bolt"
)

const (
	monthTemps = `Temps` //temps consolidated by month and year
	monthComp  = `Comp`  //compressor action by month and year
	layout     = "Jan2006"
)

var (
	bktTemp = []byte(monthTemps)
	bktComp = []byte(monthComp)

	errNotOpen  = errors.New("Not Open")
	errNotReady = errors.New("Not Ready")

	nilTime = time.Time{}
)

type compressorStartStop struct {
	Start, Stop time.Time
}

type tempStamp struct {
	Temps map[string]float32
	TS    time.Time
}

type kegTracker struct {
	mtx             *sync.Mutex
	todayCompressor []compressorStartStop
	todayTemps      []tempStamp
	lastCompressor  time.Time
	lastTemp        time.Time
	db              *bolt.DB
}

func NewTracker(dbpath string) (*kegTracker, error) {
	bdb, err := bolt.Open(dbpath, 0600, nil)
	if err != nil {
		return nil, err
	}
	if err := bdb.Update(func(tx *bolt.Tx) error {
		if _, lerr := tx.CreateBucketIfNotExists(bktTemp); lerr != nil {
			return lerr
		}
		if _, lerr := tx.CreateBucketIfNotExists(bktComp); lerr != nil {
			return lerr
		}
		return nil
	}); err != nil {
		bdb.Close()
		return nil, err
	}
	return &kegTracker{
		mtx: &sync.Mutex{},
		db:  bdb,
	}, nil
}

func (kt *kegTracker) Close() error {
	kt.mtx.Lock()
	defer kt.mtx.Unlock()
	if kt.db == nil {
		return errNotOpen
	}
	if err := kt.nlFlush(); err != nil {
		return err
	}
	if err := kt.db.Close(); err != nil {
		return err
	}
	kt.db = nil
	return nil
}

func (kt *kegTracker) AddTemps(ts time.Time, t map[string]float32) error {
	kt.mtx.Lock()
	defer kt.mtx.Unlock()
	if kt.db == nil {
		return errNotOpen
	}
	if len(t) == 0 {
		return errors.New("zero length temperature slice")
	}
	//check if we need to flush "today" or just append
	if kt.lastTemp.YearDay() != ts.YearDay() || kt.lastTemp.Year() != ts.Year() {
		//flush temps will push the temperatures for the day to the current month
		//and clear out the current list for the day
		if err := kt.nlFlushTemps(); err != nil {
			return err
		}
	}
	kt.todayTemps = append(kt.todayTemps, tempStamp{t, ts})
	kt.lastTemp = ts
	return nil
}

func (kt *kegTracker) AddCompressor(start, stop time.Time) error {
	kt.mtx.Lock()
	defer kt.mtx.Unlock()
	if kt.db == nil {
		return errNotOpen
	}
	//check if we need to flush "today" or just append
	if kt.lastCompressor.YearDay() != stop.YearDay() || kt.lastCompressor.Year() != stop.Year() {
		//flush temps will push the temperatures for the day to the current month
		//and clear out the current list for the day
		if err := kt.nlFlushCompressor(); err != nil {
			return err
		}
	}
	kt.todayCompressor = append(kt.todayCompressor, compressorStartStop{start, stop})
	kt.lastCompressor = stop
	return nil
}

func (kt *kegTracker) GetCompressor(ts time.Time) ([]compressorStartStop, error) {
	kt.mtx.Lock()
	defer kt.mtx.Unlock()
	if kt.db == nil {
		return nil, errNotOpen
	}
	var value []compressorStartStop
	key := makeKey(ts)
	if err := kt.getDb(bktComp, key, &value); err != nil {
		return nil, err
	}
	return value, nil
}

//GetTemps retrieves all the temperature readings for the month that
//the timestamp lands in.  The timestamp will not retrieve an exact
//month
func (kt *kegTracker) GetTemps(ts time.Time) ([]tempStamp, error) {
	kt.mtx.Lock()
	defer kt.mtx.Unlock()
	if kt.db == nil {
		return nil, errNotOpen
	}
	var value []tempStamp
	key := makeKey(ts)
	if err := kt.getDb(bktTemp, key, &value); err != nil {
		return nil, err
	}
	return value, nil
}

//GetAllCompressor retrieves all compressor action periods and sorts them
func (kt *kegTracker) GetAllCompressor() ([]compressorStartStop, error) {
	kt.mtx.Lock()
	defer kt.mtx.Unlock()
	var retCss []compressorStartStop

	err := kt.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bktComp)
		c := b.Cursor()
		for k, css := c.First(); k != nil; k, css = c.Next() {
			css, err := decodeCSS(css)
			if err != nil {
				return err
			}
			retCss = append(retCss, css...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Sort(CSSVect(retCss))
	return retCss, nil
}

//GetAllTemps retrieves all timestamps from the backing store, sorts them
//and returns a vector of the tempstamps
func (kt *kegTracker) GetAllTemps() ([]tempStamp, error) {
	kt.mtx.Lock()
	defer kt.mtx.Unlock()
	var retStamps []tempStamp

	err := kt.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bktTemp)
		c := b.Cursor()
		for k, stamp := c.First(); k != nil; k, stamp = c.Next() {
			ts, err := decodeTempStamp(stamp)
			if err != nil {
				return err
			}
			retStamps = append(retStamps, ts...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Sort(retStampVect(retStamps))
	return retStamps, nil
}

func decodeCSS(b []byte) ([]compressorStartStop, error) {
	var css []compressorStartStop
	bb := bytes.NewBuffer(b)
	gdec := gob.NewDecoder(bb)
	if err := gdec.Decode(&css); err != nil {
		return nil, err
	}
	return css, nil
}

func decodeTempStamp(b []byte) ([]tempStamp, error) {
	var ts []tempStamp
	bb := bytes.NewBuffer(b)
	gdec := gob.NewDecoder(bb)
	if err := gdec.Decode(&ts); err != nil {
		return nil, err
	}
	return ts, nil
}

func (kt *kegTracker) getDb(bkt, key []byte, d interface{}) error {
	//check if there is already something in the DB for today
	if err := kt.db.View(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(bkt)
		currBB := bkt.Get(key)
		if currBB == nil {
			return nil
		}
		bb := bytes.NewBuffer(currBB)
		gdec := gob.NewDecoder(bb)
		if err := gdec.Decode(d); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

func (kt *kegTracker) writeDb(bkt, key []byte, val interface{}) error {
	return kt.db.Update(func(tx *bolt.Tx) error {
		bb := bytes.NewBuffer(nil)
		genc := gob.NewEncoder(bb)
		if err := genc.Encode(val); err != nil {
			return err
		}
		bkt := tx.Bucket(bkt)
		return bkt.Put(key, bb.Bytes())
	})
}

//forces all current items to the DB
func (kt *kegTracker) Flush() error {
	kt.mtx.Lock()
	defer kt.mtx.Unlock()
	if kt.db == nil {
		return errNotOpen
	}
	return kt.nlFlush()
}

func (kt *kegTracker) nlFlush() error {
	if err := kt.nlFlushTemps(); err != nil {
		return err
	}
	kt.lastTemp = nilTime
	if err := kt.nlFlushCompressor(); err != nil {
		return err
	}
	kt.lastCompressor = nilTime
	return nil
}

func (kt *kegTracker) nlFlushTemps() error {
	//if its the initialization time, just return no error
	if kt.lastTemp == nilTime {
		return nil
	}
	var curr []tempStamp
	key := makeKey(kt.lastTemp)
	if err := kt.getDb(bktTemp, key, &curr); err != nil {
		return err
	}

	if curr == nil {
		curr = kt.todayTemps
	} else {
		curr = append(curr, kt.todayTemps...)
	}
	kt.todayTemps = nil
	return kt.writeDb(bktTemp, key, curr)
}

func (kt *kegTracker) nlFlushCompressor() error {
	//if its the initialization time, just return no error
	if kt.lastCompressor == nilTime {
		return nil
	}
	var curr []compressorStartStop
	key := makeKey(kt.lastCompressor)
	if err := kt.getDb(bktComp, key, &curr); err != nil {
		return err
	}

	if curr == nil {
		curr = kt.todayCompressor
	} else {
		curr = append(curr, kt.todayCompressor...)
	}
	kt.todayCompressor = nil
	return kt.writeDb(bktComp, key, curr)
}

func makeKey(t time.Time) []byte {
	return []byte(t.Format(layout))
}

type retStampVect []tempStamp

func (r retStampVect) Len() int           { return len(r) }
func (r retStampVect) Less(i, j int) bool { return r[i].TS.Before(r[j].TS) }
func (r retStampVect) Swap(i, j int)      { r[i], r[j] = r[j], r[i] }

type CSSVect []compressorStartStop

func (c CSSVect) Len() int           { return len(c) }
func (c CSSVect) Less(i, j int) bool { return c[i].Stop.Before(c[j].Stop) }
func (c CSSVect) Swap(i, j int)      { c[i], c[j] = c[j], c[i] }
