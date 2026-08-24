package hysteria

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/sagernet/sing/common/auth"
)

func TestServiceUpdateUsersAtomicPublication(t *testing.T) {
	t.Parallel()

	var service Service[string]
	service.UpdateUsers(
		[]string{"alice"},
		[]string{"alice-old"},
	)
	user, loaded := service.lookupUser("alice-old")
	if !loaded || user != "alice" {
		t.Fatalf("initial lookup = (%q, %v), want (%q, true)", user, loaded, "alice")
	}
	authenticatedContext := auth.ContextWithUser(context.Background(), user)

	const iterations = 5000
	start := make(chan struct{})
	failures := make(chan error, 1)
	var waitGroup sync.WaitGroup
	updateSets := []struct {
		users     []string
		passwords []string
	}{
		{
			users:     []string{"alice", "bob"},
			passwords: []string{"alice-old", "bob-password"},
		},
		{
			users:     []string{"alice"},
			passwords: []string{"alice-new"},
		},
	}

	waitGroup.Add(1)
	go func() {
		defer waitGroup.Done()
		<-start
		for index := range iterations {
			update := updateSets[index%len(updateSets)]
			service.UpdateUsers(update.users, update.passwords)
		}
	}()
	for range 8 {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			for range iterations {
				for _, lookup := range []struct {
					password string
					user     string
				}{
					{password: "alice-old", user: "alice"},
					{password: "alice-new", user: "alice"},
					{password: "bob-password", user: "bob"},
				} {
					if currentUser, found := service.lookupUser(lookup.password); found && currentUser != lookup.user {
						select {
						case failures <- fmt.Errorf("lookup returned user %q, want %q", currentUser, lookup.user):
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

	service.UpdateUsers([]string{"alice"}, []string{"alice-new"})
	if _, loaded := service.lookupUser("alice-old"); loaded {
		t.Fatal("removed authentication still succeeds")
	}
	user, loaded = service.lookupUser("alice-new")
	if !loaded || user != "alice" {
		t.Fatalf("rotated lookup = (%q, %v), want (%q, true)", user, loaded, "alice")
	}
	sessionUser, loaded := auth.UserFromContext[string](authenticatedContext)
	if !loaded || sessionUser != "alice" {
		t.Fatalf("authenticated session user = (%q, %v), want (%q, true)", sessionUser, loaded, "alice")
	}
}
