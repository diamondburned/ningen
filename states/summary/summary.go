package summary

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/state"
	"github.com/diamondburned/arikawa/v3/utils/ws"
	"github.com/diamondburned/ningen/v3/handlerrepo"
)

var maxSummaries int64 = 10

// SetMaxSummaries sets the maximum number of summaries to keep in memory.
func SetMaxSummaries(max int) {
	atomic.StoreInt64(&maxSummaries, int64(max))
}

// PersistenceMaxAge is the maximum age of a persisted summary. Summaries older
// than this will be deleted. Summaries are only deleted when a new summary is
// received.
const PersistenceMaxAge = 30 * time.Minute

// PersistenceMaxCount is the maximum number of summaries to keep on disk.
// Summaries are only deleted when a new summary is received.
const PersistenceMaxCount = 50

type State struct {
	mutex     sync.RWMutex
	state     *state.State
	summaries map[discord.ChannelID][]gateway.ConversationSummary
}

func NewState(state *state.State, r handlerrepo.AddHandler) *State {
	s := &State{
		state:     state,
		summaries: make(map[discord.ChannelID][]gateway.ConversationSummary),
	}

	r.AddSyncHandler(func(u *gateway.ConversationSummaryUpdateEvent) {
		s.mutex.Lock()
		defer s.mutex.Unlock()
		s.summaries[u.ChannelID] = insertSummaries(s.summaries[u.ChannelID], u.Summaries...)
	})

	var lastCleanMutex sync.Mutex
	lastClean := make(map[discord.ChannelID]time.Time)

	shouldClean := func(now time.Time, chID discord.ChannelID) bool {
		lastCleanMutex.Lock()
		defer lastCleanMutex.Unlock()

		if last, ok := lastClean[chID]; ok && now.Sub(last) < PersistenceMaxAge {
			return false
		}

		lastClean[chID] = now
		return true
	}

	var persistentPath string
	persistentPathInit := sync.OnceFunc(func() {
		cacheDir, err := os.UserCacheDir()
		if err != nil {
			log.Println("ningen: summary: failed to get user cache directory:", err)
			return
		}
		persistentPath = filepath.Join(cacheDir, "ningen", "summary")
	})

	r.AddHandler(func(u *gateway.ConversationSummaryUpdateEvent) {
		persistentPathInit()
		if persistentPath == "" {
			return
		}

		chPath := filepath.Join(persistentPath, u.ChannelID.String())
		if err := os.MkdirAll(chPath, 0755); err != nil {
			log.Println("ningen: summary: failed to create state directory:", err)
			return
		}

		for _, summary := range u.Summaries {
			data, err := json.Marshal(summary)
			if err != nil {
				log.Println("ningen: summary: failed to marshal summary:", err)
				continue
			}

			filePath := filepath.Join(chPath, summary.ID.String()+".json")
			if err := writeToFile(filePath, data); err != nil {
				log.Println("ningen: summary: failed to write summary:", err)
				continue
			}
		}

		now := time.Now()
		if !shouldClean(now, u.ChannelID) {
			return
		}

		files, err := os.ReadDir(chPath)
		if err != nil {
			log.Println("ningen: summary: failed to read directory for clean up:", err)
			return
		}

		fileIDs := make(map[os.DirEntry]discord.Snowflake, len(files))
		for _, file := range files {
			id, err := discord.ParseSnowflake(strings.TrimSuffix(file.Name(), ".json"))
			if err != nil {
				log.Println("ningen: summary: failed to parse summary ID for clean up:", err)
				continue
			}
			fileIDs[file] = id
		}

		slices.SortFunc(files, func(a, b os.DirEntry) int {
			return int(fileIDs[a] - fileIDs[b])
		})

		var deleted int
		var kept int

		// Traverse from the end to the beginning so that we can delete the
		// oldest summaries first.
		for i := len(files) - 1; i >= 0; i-- {
			file := files[i]

			if kept < PersistenceMaxCount {
				if fileIDs[file].Time().Add(PersistenceMaxAge).After(now) {
					kept++
					continue
				}
			}

			deleted++
			if err := os.Remove(filepath.Join(chPath, file.Name())); err != nil {
				log.Println("ningen: summary: failed to remove file for clean up:", err)
			}
		}

		if deleted == len(files) {
			if err := os.Remove(chPath); err != nil {
				log.Println("ningen: summary: failed to remove empty directory for clean up:", err)
			}
		}
	})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()

		persistentPathInit()
		if persistentPath == "" {
			return
		}

		chDirs, err := os.ReadDir(persistentPath)
		if err != nil {
			if !os.IsNotExist(err) {
				log.Println("ningen: summary: failed to read directory for loading:", err)
			}
			return
		}

		for _, chDir := range chDirs {
			snowflake, err := discord.ParseSnowflake(chDir.Name())
			if err != nil {
				log.Println("ningen: summary: failed to parse channel ID for loading:", err)
				continue
			}
			chID := discord.ChannelID(snowflake)

			summaryFiles, err := os.ReadDir(filepath.Join(persistentPath, chDir.Name()))
			if err != nil {
				log.Println("ningen: summary: failed to read directory for loading:", err)
				continue
			}
			if len(summaryFiles) == 0 {
				continue
			}

			summaries := make([]gateway.ConversationSummary, 0, len(summaryFiles))
			for _, summaryFile := range summaryFiles {
				summaryPath := filepath.Join(persistentPath, chDir.Name(), summaryFile.Name())
				summary, err := readSummary(summaryPath)
				if err != nil {
					log.Println("ningen: summary: failed to read summary for loading:", err)
					continue
				}
				summaries = append(summaries, *summary)
			}

			s.mutex.Lock()
			s.summaries[chID] = insertSummaries(s.summaries[chID], summaries...)
			s.mutex.Unlock()
		}
	}()

	r.AddSyncHandler(func(*ws.CloseEvent) {
		wg.Wait()
	})

	return s
}

func insertSummaries(summaries []gateway.ConversationSummary, more ...gateway.ConversationSummary) []gateway.ConversationSummary {
	for _, summary := range more {
		ix, ok := slices.BinarySearchFunc(summaries, summary.EndID,
			func(s gateway.ConversationSummary, msgID discord.MessageID) int {
				return int(s.EndID - msgID)
			},
		)
		if ok {
			summaries[ix] = summary
		} else {
			summaries = slices.Insert(summaries, ix, summary)
		}
	}
	if len(summaries) > int(maxSummaries) {
		summaries = slices.Delete(summaries, 0, len(summaries)-int(maxSummaries))
	}
	return summaries
}

func readSummary(path string) (*gateway.ConversationSummary, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	var s gateway.ConversationSummary
	if err := json.NewDecoder(f).Decode(&s); err != nil {
		return nil, fmt.Errorf("failed to decode summary: %w", err)
	}

	return &s, nil
}

func writeToFile(path string, data []byte) error {
	if runtime.GOOS == "windows" {
		return os.WriteFile(path, data, 0600)
	}

	baseDir := filepath.Dir(path)

	tmp, err := os.CreateTemp(baseDir, "tmp")
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %w", err)
	}
	tmpName := tmp.Name()
	defer tmp.Close()

	if _, err := tmp.Write(data); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("failed to write to temporary file: %w", err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("failed to rename temporary file: %w", err)
	}

	return nil
}

// Summaries returns the summaries for the given channel.
func (s *State) Summaries(channelID discord.ChannelID) []gateway.ConversationSummary {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	return s.summaries[channelID]
}

// LastSummary returns the last summary for the given channel.
func (s *State) LastSummary(channelID discord.ChannelID) *gateway.ConversationSummary {
	summaries := s.Summaries(channelID)
	if len(summaries) == 0 {
		return nil
	}
	return &summaries[len(summaries)-1]
}
