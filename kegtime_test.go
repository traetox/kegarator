package main

import (
	"fmt"
	"log"
	"os"
	"testing"
	"time"
)

const (
	dbPath     = `/tmp/kegtest.db`
	testCount  = 100
	innerCount = 10
	longForm   = "Jan 2, 2006 at 3:04pm (MST)"
)

var (
	baseTemp = float32(0.0)
	baseTime time.Time
)

func init() {
	var err error
	baseTime, err = time.Parse(longForm, "Feb 3, 2013 at 7:54pm (PST)")
	if err != nil {
		log.Fatal("failed to create base time", err)
	}
}

func TestInit(t *testing.T) {
	tr, err := NewTracker(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAddTemps(t *testing.T) {
	tr, err := NewTracker(dbPath)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < testCount; i++ {
		temps := make(map[string]float32, 1)
		for j := 0; j < (i%10 + 1); j++ {
			temps[fmt.Sprintf("%d", j)] = baseTemp + float32(i) + float32(j)/10
		}
		if err := tr.AddTemps(baseTime.Add(time.Second*time.Duration(i)), temps); err != nil {
			t.Fatal(err)
		}
	}

	if err := tr.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestGetTemps(t *testing.T) {
	tr, err := NewTracker(dbPath)
	if err != nil {
		t.Fatal(err)
	}

	temps, err := tr.GetAllTemps()
	if err != nil {
		t.Fatal(err)
	}
	if len(temps) != testCount {
		t.Fatal(fmt.Errorf("Failed to get all temps back: %d != %d", len(temps), testCount))
	}
	for i := range temps {
		if temps[i].TS.Unix() != baseTime.Add(time.Second*time.Duration(i)).Unix() {
			t.Fatal(fmt.Errorf("Invalid timestamp: %d != %d", temps[i].TS.Unix(),
				baseTime.Add(time.Second*time.Duration(i)).Unix()))
		}
		if len(temps[i].Temps) != (i%10 + 1) {
			t.Fatal(fmt.Errorf("Invalid temp count: %d != %d", len(temps[i].Temps), (i % 10)))
		}
	}

	if err := tr.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestClean(t *testing.T) {
	if err := os.RemoveAll(dbPath); err != nil {
		t.Fatal(err)
	}
}
