// Manual WS presence test: two clients connect with JWTs, the test
// asserts that (a) online_count reflects the live connections, and
// (b) a presence event is delivered to the friend when the other
// connects.
//
//	go run ./testtools/wspresence -base http://127.0.0.1:8769 \
//	    -secret dev-jwt-secret-change-me \
//	    -service-token dev-service-token-change-me \
//	    -a <uuid-a> -b <uuid-b>
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/golang-jwt/jwt/v5"
)

func main() {
	base := flag.String("base", "http://127.0.0.1:8769", "base URL")
	secret := flag.String("secret", "dev-jwt-secret-change-me", "JWT secret")
	serviceToken := flag.String("service-token", "dev-service-token-change-me", "service token")
	accountA := flag.String("a", "", "account A UUID (already friends with B)")
	accountB := flag.String("b", "", "account B UUID (already friends with A)")
	flag.Parse()

	if *accountA == "" || *accountB == "" {
		fmt.Fprintln(os.Stderr, "-a and -b are required")
		os.Exit(2)
	}

	tokenA := signJWT(*secret, *accountA)
	tokenB := signJWT(*secret, *accountB)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(*base, "http") + "/ws?token=" + tokenA
	connA, _, err := websocket.Dial(ctx, wsURL, nil)
	must(err, "dial A")
	defer connA.Close(websocket.StatusNormalClosure, "bye")
	fmt.Println("A connected")

	// Give the host a moment to Register before B joins so we can
	// observe A receiving B's "friend_online" on the same socket.
	time.Sleep(200 * time.Millisecond)

	count := onlineCount(*base, *serviceToken)
	fmt.Printf("online_count after A connects = %d\n", count)
	if count != 1 {
		fmt.Fprintln(os.Stderr, "want 1")
		os.Exit(1)
	}

	// B connects; A should receive a friend_online for B.
	wsURLB := "ws" + strings.TrimPrefix(*base, "http") + "/ws?token=" + tokenB
	connB, _, err := websocket.Dial(ctx, wsURLB, nil)
	must(err, "dial B")
	defer connB.Close(websocket.StatusNormalClosure, "bye")
	fmt.Println("B connected")

	readCtx, readCancel := context.WithTimeout(ctx, 5*time.Second)
	defer readCancel()
	_, data, err := connA.Read(readCtx)
	must(err, "A read")
	fmt.Printf("A received: %s\n", data)

	var msg map[string]string
	_ = json.Unmarshal(data, &msg)
	if msg["type"] != "friend_online" || msg["account_id"] != *accountB {
		fmt.Fprintf(os.Stderr, "want friend_online from %s, got %s from %s\n", *accountB, msg["type"], msg["account_id"])
		os.Exit(1)
	}

	count = onlineCount(*base, *serviceToken)
	fmt.Printf("online_count with both connected = %d\n", count)
	if count != 2 {
		fmt.Fprintln(os.Stderr, "want 2")
		os.Exit(1)
	}

	// B disconnects; A should see friend_offline.
	_ = connB.Close(websocket.StatusNormalClosure, "leaving")
	readCtx2, readCancel2 := context.WithTimeout(ctx, 5*time.Second)
	defer readCancel2()
	_, data, err = connA.Read(readCtx2)
	must(err, "A read after B close")
	fmt.Printf("A received: %s\n", data)

	_ = json.Unmarshal(data, &msg)
	if msg["type"] != "friend_offline" || msg["account_id"] != *accountB {
		fmt.Fprintf(os.Stderr, "want friend_offline from %s, got %s from %s\n", *accountB, msg["type"], msg["account_id"])
		os.Exit(1)
	}

	fmt.Println("PASS")
}

func signJWT(secret, accountID string) string {
	claims := jwt.MapClaims{
		"account_id": accountID,
		"session_id": "00000000-0000-0000-0000-000000000001",
		"exp":        time.Now().Add(time.Hour).Unix(),
		"iat":        time.Now().Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := token.SignedString([]byte(secret))
	must(err, "sign")
	return s
}

func onlineCount(base, serviceToken string) int {
	req, _ := http.NewRequest("GET", base+"/internal/presence/count", nil)
	req.Header.Set("X-Service-Token", serviceToken)
	resp, err := http.DefaultClient.Do(req)
	must(err, "count")
	defer resp.Body.Close()
	var body struct {
		OnlineCount int `json:"online_count"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return body.OnlineCount
}

func must(err error, ctx string) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", ctx, err)
		os.Exit(1)
	}
}
