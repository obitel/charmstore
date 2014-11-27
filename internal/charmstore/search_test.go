// Copyright 2014 Canonical Ltd.
// Licensed under the LGPLv3, see LICENCE file for details.

package charmstore

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"

	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"
	"gopkg.in/juju/charm.v4"

	"github.com/juju/charmstore/internal/mongodoc"
	"github.com/juju/charmstore/internal/storetesting"
)

type StoreSearchSuite struct {
	storetesting.IsolatedMgoESSuite
	store *Store
	index SearchIndex
}

var _ = gc.Suite(&StoreSearchSuite{})

func (s *StoreSearchSuite) SetUpTest(c *gc.C) {
	s.IsolatedMgoESSuite.SetUpTest(c)
	s.index = SearchIndex{s.ES, s.TestIndex}
	s.ES.RefreshIndex(".versions")
	store, err := NewStore(s.Session.DB("foo"), &s.index)
	s.addCharmsToStore(c, store)
	c.Assert(err, gc.IsNil)
	s.store = store
}

var exportTestCharms = map[string]string{
	"wordpress": "cs:precise/wordpress-23",
	"mysql":     "cs:trusty/mysql-7",
	"varnish":   "cs:~foo/trusty/varnish-1",
}

var exportTestBundles = map[string]string{
	"wordpress-simple": "cs:bundle/wordpress-simple-4",
}

func (s *StoreSearchSuite) TestSuccessfulExport(c *gc.C) {
	for _, ref := range exportTestCharms {
		var entity *mongodoc.Entity
		err := s.store.DB.Entities().FindId(ref).One(&entity)
		c.Assert(err, gc.IsNil)
		var actual json.RawMessage
		err = s.store.ES.GetDocument(s.TestIndex, typeName, s.store.ES.getID(entity.URL), &actual)
		c.Assert(err, gc.IsNil)
		c.Assert([]byte(actual), storetesting.JSONEquals, entity)
	}
}

func (s *StoreSearchSuite) TestNoExportDeprecated(c *gc.C) {
	charmArchive := storetesting.Charms.CharmDir("mysql")
	url := charm.MustParseReference("cs:saucy/mysql-4")
	err := s.store.AddCharmWithArchive(url, charmArchive)
	c.Assert(err, gc.IsNil)

	var entity *mongodoc.Entity
	err = s.store.DB.Entities().FindId("cs:trusty/mysql-7").One(&entity)
	c.Assert(err, gc.IsNil)
	present, err := s.store.ES.HasDocument(s.TestIndex, typeName, s.store.ES.getID(entity.URL))
	c.Assert(err, gc.IsNil)
	c.Assert(present, gc.Equals, true)

	err = s.store.DB.Entities().FindId("cs:saucy/mysql-4").One(&entity)
	c.Assert(err, gc.IsNil)
	present, err = s.store.ES.HasDocument(s.TestIndex, typeName, s.store.ES.getID(entity.URL))
	c.Assert(err, gc.IsNil)
	c.Assert(present, gc.Equals, false)
}

func (s *StoreSearchSuite) TestExportOnlyLatest(c *gc.C) {
	charmArchive := storetesting.Charms.CharmDir("wordpress")
	url := charm.MustParseReference("cs:precise/wordpress-22")
	err := s.store.AddCharmWithArchive(url, charmArchive)
	c.Assert(err, gc.IsNil)
	var expected, old *mongodoc.Entity
	var actual json.RawMessage
	err = s.store.DB.Entities().FindId("cs:precise/wordpress-22").One(&old)
	c.Assert(err, gc.IsNil)
	err = s.store.DB.Entities().FindId("cs:precise/wordpress-23").One(&expected)
	c.Assert(err, gc.IsNil)
	err = s.store.ES.GetDocument(s.TestIndex, typeName, s.store.ES.getID(old.URL), &actual)
	c.Assert(err, gc.IsNil)
	c.Assert([]byte(actual), storetesting.JSONEquals, expected)
}

func (s *StoreSearchSuite) addCharmsToStore(c *gc.C, store *Store) {
	for name, ref := range exportTestCharms {
		charmArchive := storetesting.Charms.CharmDir(name)
		url := charm.MustParseReference(ref)
		charmArchive.Meta().Categories = strings.Split(name, "-")
		err := store.AddCharmWithArchive(url, charmArchive)
		c.Assert(err, gc.IsNil)
	}
	for name, ref := range exportTestBundles {
		bundleArchive := storetesting.Charms.BundleDir(name)
		url := charm.MustParseReference(ref)
		bundleArchive.Data().Tags = strings.Split(name, "-")
		err := store.AddBundleWithArchive(url, bundleArchive)
		c.Assert(err, gc.IsNil)
	}
}

var searchTests = []struct {
	about   string
	sp      SearchParams
	results []string
}{
	{
		about: "basic text search",
		sp: SearchParams{
			Text: "wordpress",
		},
		results: []string{
			exportTestCharms["wordpress"],
			exportTestBundles["wordpress-simple"],
		},
	}, {
		about: "blank text search",
		sp: SearchParams{
			Text: "",
		},
		results: []string{
			exportTestCharms["wordpress"],
			exportTestCharms["mysql"],
			exportTestCharms["varnish"],
			exportTestBundles["wordpress-simple"],
		},
	}, {
		about: "autocomplete search",
		sp: SearchParams{
			Text:         "word",
			AutoComplete: true,
		},
		results: []string{
			exportTestCharms["wordpress"],
			exportTestBundles["wordpress-simple"],
		},
	}, {
		about: "description filter search",
		sp: SearchParams{
			Text: "",
			Filters: map[string][]string{
				"description": {"blog"},
			},
		},
		results: []string{
			exportTestCharms["wordpress"],
		},
	}, {
		about: "name filter search",
		sp: SearchParams{
			Text: "",
			Filters: map[string][]string{
				"name": {"wordpress"},
			},
		},
		results: []string{
			exportTestCharms["wordpress"],
		},
	}, {
		about: "owner filter search",
		sp: SearchParams{
			Text: "",
			Filters: map[string][]string{
				"owner": {"foo"},
			},
		},
		results: []string{
			exportTestCharms["varnish"],
		},
	}, {
		about: "provides filter search",
		sp: SearchParams{
			Text: "",
			Filters: map[string][]string{
				"provides": {"mysql"},
			},
		},
		results: []string{
			exportTestCharms["mysql"],
		},
	}, {
		about: "requires filter search",
		sp: SearchParams{
			Text: "",
			Filters: map[string][]string{
				"requires": {"mysql"},
			},
		},
		results: []string{
			exportTestCharms["wordpress"],
		},
	}, {
		about: "series filter search",
		sp: SearchParams{
			Text: "",
			Filters: map[string][]string{
				"series": {"trusty"},
			},
		},
		results: []string{
			exportTestCharms["mysql"],
			exportTestCharms["varnish"],
		},
	}, {
		about: "summary filter search",
		sp: SearchParams{
			Text: "",
			Filters: map[string][]string{
				"summary": {"Database engine"},
			},
		},
		results: []string{
			exportTestCharms["mysql"],
			exportTestCharms["varnish"],
		},
	}, {
		about: "tags filter search",
		sp: SearchParams{
			Text: "",
			Filters: map[string][]string{
				"tags": {"wordpress"},
			},
		},
		results: []string{
			exportTestCharms["wordpress"],
			exportTestBundles["wordpress-simple"],
		},
	}, {
		about: "bundle type filter search",
		sp: SearchParams{
			Text: "",
			Filters: map[string][]string{
				"type": {"bundle"},
			},
		},
		results: []string{
			exportTestBundles["wordpress-simple"],
		},
	}, {
		about: "charm type filter search",
		sp: SearchParams{
			Text: "",
			Filters: map[string][]string{
				"type": {"charm"},
			},
		},
		results: []string{
			exportTestCharms["wordpress"],
			exportTestCharms["mysql"],
			exportTestCharms["varnish"],
		},
	}, {
		about: "charm & bundle type filter search",
		sp: SearchParams{
			Text: "",
			Filters: map[string][]string{
				"type": {"charm", "bundle"},
			},
		},
		results: []string{
			exportTestCharms["wordpress"],
			exportTestCharms["mysql"],
			exportTestCharms["varnish"],
			exportTestBundles["wordpress-simple"],
		},
	}, {
		about: "invalid filter search",
		sp: SearchParams{
			Text: "",
			Filters: map[string][]string{
				"no such filter": {"foo"},
			},
		},
		results: []string{
			exportTestCharms["wordpress"],
			exportTestCharms["mysql"],
			exportTestCharms["varnish"],
			exportTestBundles["wordpress-simple"],
		},
	}, {
		about: "valid & invalid filter search",
		sp: SearchParams{
			Text: "",
			Filters: map[string][]string{
				"no such filter": {"foo"},
				"type":           {"charm"},
			},
		},
		results: []string{
			exportTestCharms["wordpress"],
			exportTestCharms["mysql"],
			exportTestCharms["varnish"],
		},
	}, {
		about: "paginated search",
		sp: SearchParams{
			Filters: map[string][]string{
				"name": {"mysql"},
			},
			Skip: 1,
		},
		results: []string{},
	},
}

func (s *StoreSearchSuite) TestSearches(c *gc.C) {
	s.store.ES.Database.RefreshIndex(s.TestIndex)
	for i, test := range searchTests {
		c.Logf("test %d: %s", i, test.about)
		res, err := s.store.Search(test.sp)
		c.Assert(err, gc.IsNil)
		assertSearchResults(c, res, test.results)
	}
}

func (s *StoreSearchSuite) TestPaginatedSearch(c *gc.C) {
	err := s.store.ES.Database.RefreshIndex(s.TestIndex)
	c.Assert(err, gc.IsNil)
	sp := SearchParams{
		Text: "wordpress",
		Skip: 1,
	}
	res, err := s.store.Search(sp)
	c.Assert(err, gc.IsNil)
	c.Assert(res.Results, gc.HasLen, 1)
	c.Assert(res.Total, gc.Equals, 2)
}

func (s *StoreSearchSuite) TestLimitTestSearch(c *gc.C) {
	err := s.store.ES.Database.RefreshIndex(s.TestIndex)
	c.Assert(err, gc.IsNil)
	sp := SearchParams{
		Text:  "wordpress",
		Limit: 1,
	}
	res, err := s.store.Search(sp)
	c.Assert(err, gc.IsNil)
	c.Assert(res.Results, gc.HasLen, 1)
}

func (s *StoreSearchSuite) TestPromulgatedRank(c *gc.C) {
	charmArchive := storetesting.Charms.CharmDir("varnish")
	url := charm.MustParseReference("cs:trusty/varnish-1")
	s.store.AddCharmWithArchive(url, charmArchive)
	s.store.ES.Database.RefreshIndex(s.TestIndex)
	sp := SearchParams{
		Filters: map[string][]string{
			"name": {"varnish"},
		},
	}
	res, err := s.store.Search(sp)
	c.Assert(err, gc.IsNil)
	c.Assert(res.Results, gc.HasLen, 2)
	c.Assert(res.Results[0].String(), gc.Equals, "cs:trusty/varnish-1")
	c.Assert(res.Results[1].String(), gc.Equals, exportTestCharms["varnish"])
}

func (s *StoreSearchSuite) TestSorting(c *gc.C) {
	s.store.ES.Database.RefreshIndex(s.TestIndex)
	tests := []struct {
		about     string
		sortQuery string
		results   []string
	}{{
		about:     "name ascending",
		sortQuery: "name",
		results: []string{
			exportTestCharms["mysql"],
			exportTestCharms["varnish"],
			exportTestCharms["wordpress"],
			exportTestBundles["wordpress-simple"],
		},
	}, {
		about:     "name descending",
		sortQuery: "-name",
		results: []string{
			exportTestBundles["wordpress-simple"],
			exportTestCharms["wordpress"],
			exportTestCharms["varnish"],
			exportTestCharms["mysql"],
		},
	}, {
		about:     "series ascending",
		sortQuery: "series,name",
		results: []string{
			exportTestBundles["wordpress-simple"],
			exportTestCharms["wordpress"],
			exportTestCharms["mysql"],
			exportTestCharms["varnish"],
		},
	}, {
		about:     "series descending",
		sortQuery: "-series,name",
		results: []string{
			exportTestCharms["mysql"],
			exportTestCharms["varnish"],
			exportTestCharms["wordpress"],
			exportTestBundles["wordpress-simple"],
		},
	}, {
		about:     "owner ascending",
		sortQuery: "owner,name",
		results: []string{
			exportTestCharms["mysql"],
			exportTestCharms["wordpress"],
			exportTestBundles["wordpress-simple"],
			exportTestCharms["varnish"],
		},
	}, {
		about:     "owner descending",
		sortQuery: "-owner,name",
		results: []string{
			exportTestCharms["varnish"],
			exportTestCharms["mysql"],
			exportTestCharms["wordpress"],
			exportTestBundles["wordpress-simple"],
		},
	}}
	for i, test := range tests {
		c.Logf("test %d. %s", i, test.about)
		var sp SearchParams
		err := sp.ParseSortFields(test.sortQuery)
		c.Assert(err, gc.IsNil)
		res, err := s.store.Search(sp)
		c.Assert(err, gc.IsNil)
		c.Assert(res.Results, gc.HasLen, len(test.results))
		for i, ref := range res.Results {
			c.Assert(ref.String(), gc.Equals, test.results[i])
		}
	}
}

func (s *StoreSearchSuite) TestBoosting(c *gc.C) {
	s.store.ES.Database.RefreshIndex(s.TestIndex)
	var sp SearchParams
	res, err := s.store.Search(sp)
	c.Assert(err, gc.IsNil)
	c.Assert(res.Results, gc.HasLen, 4)
	c.Assert(res.Results, jc.DeepEquals, []*charm.Reference{
		charm.MustParseReference(exportTestBundles["wordpress-simple"]),
		charm.MustParseReference(exportTestCharms["mysql"]),
		charm.MustParseReference(exportTestCharms["wordpress"]),
		charm.MustParseReference(exportTestCharms["varnish"]),
	})
}

func (s *StoreSearchSuite) TestEnsureIndex(c *gc.C) {
	s.store.ES.Index = s.TestIndex + "-ensure-index"
	defer s.ES.DeleteDocument(".versions", "version", s.store.ES.Index)
	indexes, err := s.ES.ListIndexesForAlias(s.store.ES.Index)
	c.Assert(err, gc.Equals, nil)
	c.Assert(indexes, gc.HasLen, 0)
	err = s.store.ES.ensureIndexes(false)
	c.Assert(err, gc.Equals, nil)
	indexes, err = s.ES.ListIndexesForAlias(s.store.ES.Index)
	c.Assert(err, gc.Equals, nil)
	c.Assert(indexes, gc.HasLen, 1)
	index := indexes[0]
	err = s.store.ES.ensureIndexes(false)
	c.Assert(err, gc.Equals, nil)
	indexes, err = s.ES.ListIndexesForAlias(s.store.ES.Index)
	c.Assert(err, gc.Equals, nil)
	c.Assert(indexes, gc.HasLen, 1)
	c.Assert(indexes[0], gc.Equals, index)
}

func (s *StoreSearchSuite) TestEnsureConcurrent(c *gc.C) {
	s.store.ES.Index = s.TestIndex + "-ensure-index-conc"
	defer s.ES.DeleteDocument(".versions", "version", s.store.ES.Index)
	indexes, err := s.ES.ListIndexesForAlias(s.store.ES.Index)
	c.Assert(err, gc.Equals, nil)
	c.Assert(indexes, gc.HasLen, 0)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		err = s.store.ES.ensureIndexes(false)
		c.Check(err, gc.Equals, nil)
		wg.Done()
	}()
	err = s.store.ES.ensureIndexes(false)
	c.Assert(err, gc.Equals, nil)
	indexes, err = s.ES.ListIndexesForAlias(s.store.ES.Index)
	c.Assert(err, gc.Equals, nil)
	c.Assert(indexes, gc.HasLen, 1)
	wg.Wait()
}

func (s *StoreSearchSuite) TestEnsureIndexForce(c *gc.C) {
	s.store.ES.Index = s.TestIndex + "-ensure-index-force"
	defer s.ES.DeleteDocument(".versions", "version", s.store.ES.Index)
	indexes, err := s.ES.ListIndexesForAlias(s.store.ES.Index)
	c.Assert(err, gc.Equals, nil)
	c.Assert(indexes, gc.HasLen, 0)
	err = s.store.ES.ensureIndexes(false)
	c.Assert(err, gc.Equals, nil)
	indexes, err = s.ES.ListIndexesForAlias(s.store.ES.Index)
	c.Assert(err, gc.Equals, nil)
	c.Assert(indexes, gc.HasLen, 1)
	index := indexes[0]
	err = s.store.ES.ensureIndexes(true)
	c.Assert(err, gc.Equals, nil)
	indexes, err = s.ES.ListIndexesForAlias(s.store.ES.Index)
	c.Assert(err, gc.Equals, nil)
	c.Assert(indexes, gc.HasLen, 1)
	c.Assert(indexes[0], gc.Not(gc.Equals), index)
}

func (s *StoreSearchSuite) TestGetCurrentVersionNoVersion(c *gc.C) {
	s.store.ES.Index = s.TestIndex + "-current-version"
	defer s.ES.DeleteDocument(".versions", "version", s.store.ES.Index)
	v, dv, err := s.store.ES.getCurrentVersion()
	c.Assert(err, gc.Equals, nil)
	c.Assert(v, gc.Equals, version{})
	c.Assert(dv, gc.Equals, int64(0))
}

func (s *StoreSearchSuite) TestGetCurrentVersionWithVersion(c *gc.C) {
	s.store.ES.Index = s.TestIndex + "-current-version"
	defer s.ES.DeleteDocument(".versions", "version", s.store.ES.Index)
	index, err := s.store.ES.newIndex()
	c.Assert(err, gc.Equals, nil)
	updated, err := s.store.ES.updateVersion(version{1, index}, 0)
	c.Assert(err, gc.Equals, nil)
	c.Assert(updated, gc.Equals, true)
	v, dv, err := s.store.ES.getCurrentVersion()
	c.Assert(err, gc.Equals, nil)
	c.Assert(v, gc.Equals, version{1, index})
	c.Assert(dv, gc.Equals, int64(1))
}

func (s *StoreSearchSuite) TestUpdateVersionNew(c *gc.C) {
	s.store.ES.Index = s.TestIndex + "-update-version"
	defer s.ES.DeleteDocument(".versions", "version", s.store.ES.Index)
	index, err := s.store.ES.newIndex()
	c.Assert(err, gc.Equals, nil)
	updated, err := s.store.ES.updateVersion(version{1, index}, 0)
	c.Assert(err, gc.Equals, nil)
	c.Assert(updated, gc.Equals, true)
}

func (s *StoreSearchSuite) TestUpdateVersionUpdate(c *gc.C) {
	s.store.ES.Index = s.TestIndex + "-update-version"
	defer s.ES.DeleteDocument(".versions", "version", s.store.ES.Index)
	index, err := s.store.ES.newIndex()
	c.Assert(err, gc.Equals, nil)
	updated, err := s.store.ES.updateVersion(version{1, index}, 0)
	c.Assert(err, gc.Equals, nil)
	c.Assert(updated, gc.Equals, true)
	index, err = s.store.ES.newIndex()
	c.Assert(err, gc.Equals, nil)
	updated, err = s.store.ES.updateVersion(version{2, index}, 1)
	c.Assert(err, gc.Equals, nil)
	c.Assert(updated, gc.Equals, true)
}

func (s *StoreSearchSuite) TestUpdateCreateConflict(c *gc.C) {
	s.store.ES.Index = s.TestIndex + "-update-version"
	defer s.ES.DeleteDocument(".versions", "version", s.store.ES.Index)
	index, err := s.store.ES.newIndex()
	c.Assert(err, gc.Equals, nil)
	updated, err := s.store.ES.updateVersion(version{1, index}, 0)
	c.Assert(err, gc.Equals, nil)
	c.Assert(updated, gc.Equals, true)
	index, err = s.store.ES.newIndex()
	c.Assert(err, gc.Equals, nil)
	updated, err = s.store.ES.updateVersion(version{1, index}, 0)
	c.Assert(err, gc.Equals, nil)
	c.Assert(updated, gc.Equals, false)
}

func (s *StoreSearchSuite) TestUpdateConflict(c *gc.C) {
	s.store.ES.Index = s.TestIndex + "-update-version"
	defer s.ES.DeleteDocument(".versions", "version", s.store.ES.Index)
	index, err := s.store.ES.newIndex()
	c.Assert(err, gc.Equals, nil)
	updated, err := s.store.ES.updateVersion(version{1, index}, 0)
	c.Assert(err, gc.Equals, nil)
	c.Assert(updated, gc.Equals, true)
	index, err = s.store.ES.newIndex()
	c.Assert(err, gc.Equals, nil)
	updated, err = s.store.ES.updateVersion(version{1, index}, 3)
	c.Assert(err, gc.Equals, nil)
	c.Assert(updated, gc.Equals, false)
}

// assertSearchResults checks that the results obtained from a search are the same
// as those in the expected set, but in any order.
func assertSearchResults(c *gc.C, obtained SearchResult, expected []string) {
	c.Assert(len(obtained.Results), gc.Equals, len(expected))

	sort.Strings(expected)
	var ids []string
	for _, ref := range obtained.Results {
		ids = append(ids, ref.String())
	}
	sort.Strings(ids)
	for i, v := range expected {
		c.Assert(ids[i], gc.Equals, v)
	}
}
