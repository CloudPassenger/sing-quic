package tuic

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/sagernet/sing/common/auth"
)

func TestServiceUpdateUsersAtomicPublication(t *testing.T) {
	t.Parallel()

	aliceUUID := [16]byte{1}
	bobUUID := [16]byte{2}
	legacyUUIDA := [16]byte{3}
	legacyUUIDB := [16]byte{4}

	var service Service[string]
	service.UpdateUsers(
		[]string{"alice"},
		[][16]byte{aliceUUID},
		[]string{"alice-old"},
	)
	user, password, loaded := service.lookupUser(aliceUUID)
	if !loaded || user != "alice" || password != "alice-old" {
		t.Fatalf("initial lookup = (%q, %q, %v), want (%q, %q, true)", user, password, loaded, "alice", "alice-old")
	}
	authenticatedContext := auth.ContextWithUser(context.Background(), user)

	service.UpdateUsers(
		[]string{"", ""},
		[][16]byte{legacyUUIDA, legacyUUIDB},
		[]string{"legacy-a", "legacy-b"},
	)
	for _, lookup := range []struct {
		uuid     [16]byte
		password string
	}{
		{uuid: legacyUUIDA, password: "legacy-a"},
		{uuid: legacyUUIDB, password: "legacy-b"},
	} {
		legacyUser, legacyPassword, found := service.lookupUser(lookup.uuid)
		if !found || legacyUser != "" || legacyPassword != lookup.password {
			t.Fatalf("unnamed lookup = (%q, %q, %v), want (%q, %q, true)", legacyUser, legacyPassword, found, "", lookup.password)
		}
	}

	const iterations = 5000
	start := make(chan struct{})
	failures := make(chan error, 1)
	var waitGroup sync.WaitGroup
	updateSets := []*UserState[string]{
		NewUserState(
			[]string{"alice", ""},
			[][16]byte{aliceUUID, legacyUUIDA},
			[]string{"alice-old", "legacy-a"},
		),
		NewUserState(
			[]string{"bob", ""},
			[][16]byte{bobUUID, legacyUUIDB},
			[]string{"bob-password", "legacy-b"},
		),
	}

	waitGroup.Add(1)
	go func() {
		defer waitGroup.Done()
		<-start
		for index := range iterations {
			service.UpdateUserState(updateSets[index%len(updateSets)])
		}
	}()
	for range 8 {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			for range iterations {
				for _, lookup := range []struct {
					uuid     [16]byte
					user     string
					password string
				}{
					{uuid: aliceUUID, user: "alice", password: "alice-old"},
					{uuid: bobUUID, user: "bob", password: "bob-password"},
					{uuid: legacyUUIDA, user: "", password: "legacy-a"},
					{uuid: legacyUUIDB, user: "", password: "legacy-b"},
				} {
					currentUser, currentPassword, found := service.lookupUser(lookup.uuid)
					if found && (currentUser != lookup.user || currentPassword != lookup.password) {
						select {
						case failures <- fmt.Errorf(
							"lookup returned (%q, %q), want (%q, %q)",
							currentUser,
							currentPassword,
							lookup.user,
							lookup.password,
						):
						default:
						}
						return
					}
				}
			}
		}()
	}

	close(start)
	waitGroup.Wait()
	select {
	case err := <-failures:
		t.Fatal(err)
	default:
	}

	service.UpdateUsers(
		[]string{"alice"},
		[][16]byte{aliceUUID},
		[]string{"alice-new"},
	)
	user, password, loaded = service.lookupUser(aliceUUID)
	if !loaded || user != "alice" || password != "alice-new" {
		t.Fatalf("rotated lookup = (%q, %q, %v), want (%q, %q, true)", user, password, loaded, "alice", "alice-new")
	}
	if _, _, loaded := service.lookupUser(bobUUID); loaded {
		t.Fatal("removed UUID still authenticates")
	}
	sessionUser, loaded := auth.UserFromContext[string](authenticatedContext)
	if !loaded || sessionUser != "alice" {
		t.Fatalf("authenticated session user = (%q, %v), want (%q, true)", sessionUser, loaded, "alice")
	}
}
