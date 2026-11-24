package service_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"chat/internal/model/conversation"
	"chat/internal/model/user"
	"chat/internal/repository"
	"chat/internal/service"
	"chat/internal/validation"
)

type fakeConversationGroupStore func(context.Context, string, int64, []int64, time.Time) (*conversation.Group, error)

func (fake fakeConversationGroupStore) Create(ctx context.Context, name string, ownerID int64, ids []int64, createdAt time.Time) (*conversation.Group, error) {
	return fake(ctx, name, ownerID, ids, createdAt)
}

var (
	_ service.ConversationGroupStore = (*repository.ConversationGroupStore)(nil)
)

func TestConversationServiceCreateGroup(t *testing.T) {
	for _, count := range []int{1, 49} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			ctx := t.Context()
			usernames := make([]string, count)
			wantIDs := []int64{10}
			for i := range usernames {
				usernames[i] = fmt.Sprintf(" USER_%d ", i)
				wantIDs = append(wantIDs, int64(20+i))
			}
			original := append([]string(nil), usernames...)
			lookups, creations := 0, 0
			users := fakeConversationUsers{getByUsernames: func(gotCtx context.Context, names []string) ([]user.User, error) {
				if gotCtx != ctx || len(names) != count {
					t.Fatal("unexpected lookup context or normalized username")
				}
				members := make([]user.User, count)
				for i, name := range names {
					if name != fmt.Sprintf("user_%d", i) {
						t.Fatal("unexpected normalized username")
					}
					members[count-1-i] = user.User{ID: int64(20 + i), Username: name}
				}
				lookups++
				return members, nil
			}}
			before := time.Now().UTC()
			store := fakeConversationGroupStore(func(gotCtx context.Context, name string, ownerID int64, ids []int64, createdAt time.Time) (*conversation.Group, error) {
				if gotCtx != ctx || name != "Семья" || ownerID != 10 || !reflect.DeepEqual(ids, wantIDs) {
					t.Fatal("store must receive normalized name and all members including owner")
				}
				if createdAt.Location() != time.UTC || createdAt.Before(before) || createdAt.After(time.Now().UTC()) {
					t.Fatal("expected current UTC time")
				}
				creations++
				return &conversation.Group{Conversation: conversation.Conversation{ID: int64(creations), Type: conversation.TypeGroup, CreatedAt: createdAt}, OwnerUserID: ownerID, Name: name}, nil
			})
			s := service.NewConversationService(users, nil, nil, nil, store)
			for i := 1; i <= 2; i++ {
				got, err := s.CreateGroup(ctx, 10, "\u2003 Семья \n", usernames)
				if err != nil || got == nil || got.ID != int64(i) || got.Name != "Семья" || got.OwnerUserID != 10 {
					t.Fatalf("creation = (%+v, %v), want new stored group", got, err)
				}
			}
			if !reflect.DeepEqual(usernames, original) || lookups != 2 || creations != 2 {
				t.Fatal("input must remain unchanged; each request must create a new group")
			}
		})
	}
}

func TestConversationServiceGroupInvalidInput(t *testing.T) {
	for _, tt := range []struct {
		name      string
		ownerID   int64
		groupName string
		usernames []string
		want      error
	}{
		{"zero owner", 0, "Семья", []string{"anna"}, user.ErrNotFound},
		{"negative owner", -1, "Семья", []string{"anna"}, user.ErrNotFound},
		{"blank name", 10, " \t", []string{"anna"}, validation.ErrInvalidConversationGroupName},
		{"long name", 10, strings.Repeat("я", 101), []string{"anna"}, validation.ErrInvalidConversationGroupName},
		{"no participants", 10, "Семья", nil, conversation.ErrInvalidGroupParticipants},
		{"too many participants", 10, "Семья", make([]string, 50), conversation.ErrInvalidGroupParticipants},
		{"invalid username after valid", 10, "Семья", []string{"anna", "a!"}, validation.ErrInvalidUsername},
		{"duplicate normalized username", 10, "Семья", []string{"anna", " ANNA "}, conversation.ErrInvalidGroupParticipants},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := service.NewConversationService(nil, nil, nil, nil, nil).CreateGroup(t.Context(), tt.ownerID, tt.groupName, tt.usernames)
			if got != nil || !errors.Is(err, tt.want) {
				t.Fatalf("creation = (%+v, %v), want %v", got, err, tt.want)
			}
		})
	}
}

func TestConversationServiceGroupFailures(t *testing.T) {
	dbError := errors.New("database unavailable")
	for _, tt := range []struct {
		name                            string
		memberID                        int64
		lookupError, storeError         error
		cancelBefore, cancelAfterLookup bool
		want                            error
		wantLookups, wantCreates        int
	}{
		{"owner in participants", 10, nil, nil, false, false, conversation.ErrInvalidGroupParticipants, 1, 0},
		{"unknown participant", 0, user.ErrNotFound, nil, false, false, user.ErrNotFound, 1, 0},
		{"empty lookup result", 0, nil, nil, false, false, user.ErrNotFound, 1, 0},
		{"lookup failure", 0, dbError, nil, false, false, dbError, 1, 0},
		{"store failure", 20, nil, dbError, false, false, dbError, 1, 1},
		{"no conflict recovery", 20, nil, conversation.ErrAlreadyExists, false, false, conversation.ErrAlreadyExists, 1, 1},
		{"canceled before lookup", 20, nil, nil, true, false, context.Canceled, 0, 0},
		{"canceled after lookup", 20, nil, nil, false, true, context.Canceled, 1, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tt.cancelBefore {
				cancel()
			}
			lookups, creates := 0, 0
			users := fakeConversationUsers{getByUsernames: func(context.Context, []string) ([]user.User, error) {
				lookups++
				if tt.cancelAfterLookup {
					cancel()
				}
				if tt.lookupError != nil {
					return nil, fmt.Errorf("lookup: %w", tt.lookupError)
				}
				if tt.memberID == 0 {
					return []user.User{}, nil
				}
				return []user.User{{ID: tt.memberID, Username: "anna"}}, nil
			}}
			store := fakeConversationGroupStore(func(context.Context, string, int64, []int64, time.Time) (*conversation.Group, error) {
				creates++
				return &conversation.Group{}, fmt.Errorf("store: %w", tt.storeError)
			})
			got, err := service.NewConversationService(users, nil, nil, nil, store).CreateGroup(ctx, 10, "Семья", []string{"anna"})
			if got != nil || !errors.Is(err, tt.want) || lookups != tt.wantLookups || creates != tt.wantCreates {
				t.Fatalf("result = (%+v, %v), lookups/creates=%d/%d; want %v, %d/%d", got, err, lookups, creates, tt.want, tt.wantLookups, tt.wantCreates)
			}
		})
	}
}

func TestConversationServiceGroupMissingBatchParticipant(t *testing.T) {
	lookups := 0
	users := fakeConversationUsers{getByUsernames: func(context.Context, []string) ([]user.User, error) {
		lookups++
		return []user.User{{ID: 20, Username: "anna"}}, nil
	}}
	// A missing participant must prevent creation, even when other users exist.
	got, err := service.NewConversationService(users, nil, nil, nil, nil).CreateGroup(t.Context(), 10, "Семья", []string{"anna", "missing"})
	if got != nil || !errors.Is(err, user.ErrNotFound) || lookups != 1 {
		t.Fatalf("creation = (%+v, %v), lookups=%d", got, err, lookups)
	}
}

func TestConversationServiceGetGroupByID(t *testing.T) {
	dbError := errors.New("database unavailable")
	for _, wantError := range []error{nil, conversation.ErrNotFound, dbError, context.Canceled} {
		ctx := t.Context()
		want := &conversation.Group{Conversation: conversation.Conversation{ID: 7, Type: conversation.TypeGroup}, OwnerUserID: 10, Name: "Семья"}
		called := false
		repo := fakeConversationRepository{getGroupByIDForUser: func(gotCtx context.Context, id, userID int64) (*conversation.Group, error) {
			called = true
			if gotCtx != ctx || id != 7 || userID != 10 {
				t.Fatal("incorrect lookup arguments")
			}
			if wantError != nil {
				return want, fmt.Errorf("repository: %w", wantError)
			}
			return want, nil
		}}
		got, err := service.NewConversationService(nil, repo, nil, nil, nil).GetGroupByID(ctx, 7, 10)
		if !called || !errors.Is(err, wantError) || (wantError == nil && got != want) || (wantError != nil && got != nil) {
			t.Fatalf("lookup = (%+v, %v), want error %v", got, err, wantError)
		}
	}
	for _, ids := range [][2]int64{{0, 10}, {-1, 10}, {7, 0}, {7, -1}} {
		got, err := service.NewConversationService(nil, nil, nil, nil, nil).GetGroupByID(t.Context(), ids[0], ids[1])
		if got != nil || !errors.Is(err, conversation.ErrNotFound) {
			t.Fatalf("invalid IDs: (%+v, %v)", got, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got, err := service.NewConversationService(nil, nil, nil, nil, nil).GetGroupByID(ctx, 7, 10)
	if got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled lookup: (%+v, %v)", got, err)
	}
}
