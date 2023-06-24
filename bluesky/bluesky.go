package bluesky

import (
	"context"
	"log"
	"time"

	"github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/api/bsky"
	goskyutil "github.com/bluesky-social/indigo/cmd/gosky/util"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/pkg/errors"
)

type Bluesky struct {
	Host     string
	Handle   string
	Password string
}

func (b *Bluesky) Post(text string) error {
	xrpcc := &xrpc.Client{
		Client: goskyutil.NewHttpClient(),
		Host:   b.Host,
		Auth:   &xrpc.AuthInfo{Handle: b.Handle},
	}
	auth, err := atproto.ServerCreateSession(context.TODO(), xrpcc, &atproto.ServerCreateSession_Input{
		Identifier: xrpcc.Auth.Handle,
		Password:   b.Password,
	})
	if err != nil {
		return errors.WithStack(err)
	}
	xrpcc.Auth.Did = auth.Did
	xrpcc.Auth.AccessJwt = auth.AccessJwt
	xrpcc.Auth.RefreshJwt = auth.RefreshJwt

	post := &bsky.FeedPost{
		LexiconTypeID: "app.bsky.feed.post",
		Text:          text,
		CreatedAt:     time.Now().Local().Format(time.RFC3339),
	}

	resp, err := atproto.RepoCreateRecord(context.TODO(), xrpcc, &atproto.RepoCreateRecord_Input{
		Collection: "app.bsky.feed.post",
		Repo:       xrpcc.Auth.Did,
		Record: &lexutil.LexiconTypeDecoder{
			Val: post,
		},
	})
	if err != nil {
		return errors.WithStack(err)
	}

	log.Printf("posted: %s\n", resp.Uri)

	return nil
}

func NewClient(host, handle, pass string) *Bluesky {
	return &Bluesky{Host: host, Handle: handle, Password: pass}
}
