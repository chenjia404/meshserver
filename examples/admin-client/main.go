package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	gproto "github.com/golang/protobuf/proto"
	libp2p "github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	corecrypto "github.com/libp2p/go-libp2p/core/crypto"
	corehost "github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	coreprotocol "github.com/libp2p/go-libp2p/core/protocol"
	discoveryrouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	ma "github.com/multiformats/go-multiaddr"

	"meshserver/internal/auth"
	sessionv1 "meshserver/internal/gen/proto/meshserver/session/v1"
	meshlibp2p "meshserver/internal/libp2p"
	"meshserver/internal/protocol"
)

func main() {
	var (
		serverAddr                  = flag.String("server-addr", "", "meshserver libp2p address, for example /ip4/127.0.0.1/tcp/4001/p2p/12D3Koo...")
		discover                    = flag.Bool("discover", false, "discover a meshserver peer via DHT instead of using -server-addr")
		discoverNamespace           = flag.String("discover-namespace", "meshserver", "DHT rendezvous namespace used by meshserver discovery")
		protocolID                  = flag.String("protocol-id", "/meshserver/session/1.0.0", "session protocol id")
		keyPath                     = flag.String("key", "examples/admin-client.key", "path to the client libp2p private key")
		clientAgent                 = flag.String("client-agent", "admin-client-go", "client agent string")
		spaceID                     = flag.Uint64("space-id", 0, "target space id")
		showCreateSpacePermissions  = flag.Bool("create-space-permissions", false, "whether to query create-space permissions after auth")
		permissionsSpaceID          = flag.Uint64("permissions-space-id", 0, "space_id to query space permissions after auth")
		createSpace                 = flag.Bool("create-space", false, "whether to create a new space after auth")
		newSpaceName                = flag.String("new-space-name", "AI Example Space", "name for the created space")
		newSpaceDescription         = flag.String("new-space-description", "Created by the admin client example", "description for the created space")
		newSpaceVisibility          = flag.String("new-space-visibility", "public", "visibility for the created space: public or private")
		newSpaceAllowCreate         = flag.Bool("new-space-allow-channel-creation", true, "whether the created space should allow channel creation")
		joinSpaces                  joinSpaceFlags
		targetUserID                = flag.String("target-user-id", "", "target user_id to promote or demote")
		inviteUserID                = flag.String("invite-user-id", "", "target user_id to invite into the space")
		kickUserID                  = flag.String("kick-user-id", "", "target user_id to kick from the space")
		banUserID                   = flag.String("ban-user-id", "", "target user_id to ban from the space")
		unbanUserID                 = flag.String("unban-user-id", "", "target user_id to unban from the space")
		listMembers                 = flag.Bool("list-members", false, "whether to list space members")
		listMembersAll              = flag.Bool("list-members-all", false, "whether to page through all space members")
		memberAfterID               = flag.Uint64("member-after-id", 0, "space member id to start listing after")
		memberLimit                 = flag.Uint("member-limit", 20, "page size for listing space members")
		targetRole                  = flag.String("target-role", "admin", "target role: owner, admin, member, subscriber")
		allowChannelCreation        = flag.Bool("allow-channel-creation", true, "whether the server should allow channel creation")
		createGroup                 = flag.Bool("create-group", true, "whether to create a group after the admin updates")
		groupName                   = flag.String("group-name", "AI Example Group", "name for the created group")
		groupDescription            = flag.String("group-description", "Created by the admin client example", "description for the created group")
		groupAutoDeleteChannelID    = flag.Uint64("group-auto-delete-channel-id", 0, "channel id to configure group auto delete for")
		groupAutoDeleteAfterSeconds = flag.Uint("group-auto-delete-after-seconds", 0, "auto delete period in seconds for the group")
		channelID                   = flag.Uint64("channel-id", 0, "existing channel id to subscribe and send a message to")
		messageText                 = flag.String("message", "hello from the admin client example", "text message to send")
		syncAfterSeq                = flag.Uint64("sync-after-seq", 0, "seq to start syncing from")
		requestTimeout              = flag.Duration("timeout", 20*time.Second, "total request timeout")
	)
	var dhtBootstrapPeers bootstrapPeerFlags
	flag.Var(&dhtBootstrapPeers, "dht-bootstrap-peer", "repeatable DHT bootstrap multiaddr used for discovery")
	flag.Var(&joinSpaces, "join-space", "repeatable space id to join before other actions")
	flag.Parse()

	if !*discover && strings.TrimSpace(*serverAddr) == "" {
		log.Fatal("missing -server-addr or enable -discover")
	}

	privKey, err := meshlibp2p.LoadOrCreateIdentity(*keyPath)
	if err != nil {
		log.Fatalf("load client key: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *requestTimeout)
	defer cancel()

	host, err := libp2p.New(
		libp2p.Identity(privKey),
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"),
	)
	if err != nil {
		log.Fatalf("create libp2p host: %v", err)
	}
	defer host.Close()

	var info *peer.AddrInfo
	if *discover {
		if strings.TrimSpace(*discoverNamespace) == "" {
			log.Fatal("missing -discover-namespace")
		}
		if len(dhtBootstrapPeers) == 0 {
			log.Println("no -dht-bootstrap-peer provided, using libp2p default bootstrap peers")
		}
		bootstrapPeers, err := parseBootstrapPeers(dhtBootstrapPeers)
		if err != nil {
			log.Fatalf("parse dht bootstrap peers: %v", err)
		}
		for _, bootstrapPeer := range bootstrapPeers {
			if err := connectPeer(ctx, host, bootstrapPeer); err != nil {
				log.Printf("connect dht bootstrap peer %s failed: %v", bootstrapPeer.ID.String(), err)
			}
		}
		dhtNode, err := dht.New(ctx, host, dht.Mode(dht.ModeAuto), dht.BootstrapPeers(bootstrapPeers...))
		if err != nil {
			log.Fatalf("create dht: %v", err)
		}
		defer dhtNode.Close()
		if err := dhtNode.Bootstrap(ctx); err != nil {
			log.Fatalf("bootstrap dht: %v", err)
		}
		info, err = discoverMeshserverPeer(ctx, host, dhtNode, *discoverNamespace)
		if err != nil {
			log.Fatalf("discover meshserver peer: %v", err)
		}
		fmt.Printf("discovered meshserver peer: peer_id=%s addrs=%v namespace=%s\n", info.ID.String(), info.Addrs, *discoverNamespace)
	} else {
		info, err = addrInfo(*serverAddr)
		if err != nil {
			log.Fatalf("parse server addr: %v", err)
		}
	}
	if err := connectPeer(ctx, host, *info); err != nil {
		log.Fatalf("connect server: %v", err)
	}
	fmt.Printf("connected server peer: peer_id=%s addrs=%v\n", info.ID.String(), info.Addrs)

	stream, err := host.NewStream(ctx, info.ID, coreprotocol.ID(*protocolID))
	if err != nil {
		log.Fatalf("open stream: %v", err)
	}
	defer stream.Close()

	authResult, err := authenticate(ctx, stream, privKey, host.ID().String(), *clientAgent, *protocolID)
	if err != nil {
		log.Fatalf("authenticate: %v", err)
	}

	fmt.Printf("authenticated: user_id=%s display_name=%s session_id=%s\n", authResult.UserId, authResult.DisplayName, authResult.SessionId)
	printSpaces(authResult.Spaces)

	if *createSpace {
		vis, err := parseVisibility(*newSpaceVisibility)
		if err != nil {
			log.Fatalf("parse new space visibility: %v", err)
		}
		resp, err := createSpaceRPC(stream, *newSpaceName, *newSpaceDescription, vis, *newSpaceAllowCreate)
		if err != nil {
			log.Fatalf("create space: %v", err)
		}
		*spaceID = uint64(resp.SpaceId)
		fmt.Printf("created space: space_id=%d name=%s allow_channel_creation=%v\n", resp.SpaceId, resp.Space.Name, resp.Space.AllowChannelCreation)
	}

	if *showCreateSpacePermissions {
		resp, err := getCreateSpacePermissions(stream)
		if err != nil {
			log.Fatalf("get create-space permissions: %v", err)
		}
		fmt.Printf("create-space permissions: can_create_space=%v\n", resp.CanCreateSpace)
	}

	if *permissionsSpaceID != 0 {
		resp, err := getSpacePermissions(stream, uint32(*permissionsSpaceID))
		if err != nil {
			log.Fatalf("get space permissions: %v", err)
		}
		fmt.Printf(
			"create-group permissions: space_id=%d role=%s can_create_group=%v\n",
			resp.SpaceId,
			resp.Role.String(),
			resp.CanCreateGroup,
		)
	}

	for _, joinSpaceID := range joinSpaces {
		if joinSpaceID == 0 {
			continue
		}
		resp, err := joinSpace(stream, uint32(joinSpaceID))
		if err != nil {
			log.Fatalf("join space %d: %v", joinSpaceID, err)
		}
		fmt.Printf("joined space: space_id=%d name=%s\n", resp.SpaceId, resp.Space.Name)
	}

	if len(joinSpaces) > 0 {
		spaces, err := listSpaces(stream)
		if err != nil {
			log.Fatalf("list spaces after join: %v", err)
		}
		fmt.Println("spaces after join:")
		printSpaces(spaces)
	}

	if strings.TrimSpace(*targetUserID) != "" {
		role, err := parseMemberRole(*targetRole)
		if err != nil {
			log.Fatalf("parse target role: %v", err)
		}
		if err := setMemberRole(stream, uint32(*spaceID), *targetUserID, role); err != nil {
			log.Fatalf("set member role: %v", err)
		}
		fmt.Printf("updated member role: space_id=%d target_user_id=%s role=%s\n", *spaceID, *targetUserID, strings.ToLower(role.String()))
	}

	if strings.TrimSpace(*inviteUserID) != "" {
		resp, err := inviteSpaceMember(stream, uint32(*spaceID), *inviteUserID)
		if err != nil {
			log.Fatalf("invite space member: %v", err)
		}
		fmt.Printf("invited space member: space_id=%d target_user_id=%s name=%s\n", resp.SpaceId, resp.TargetUserId, resp.Space.Name)
	}

	if strings.TrimSpace(*kickUserID) != "" {
		resp, err := kickSpaceMember(stream, uint32(*spaceID), *kickUserID)
		if err != nil {
			log.Fatalf("kick space member: %v", err)
		}
		fmt.Printf("kicked space member: space_id=%d target_user_id=%s name=%s\n", resp.SpaceId, resp.TargetUserId, resp.Space.Name)
	}

	if strings.TrimSpace(*banUserID) != "" {
		resp, err := banSpaceMember(stream, uint32(*spaceID), *banUserID)
		if err != nil {
			log.Fatalf("ban space member: %v", err)
		}
		fmt.Printf("banned space member: space_id=%d target_user_id=%s name=%s\n", resp.SpaceId, resp.TargetUserId, resp.Space.Name)
	}

	if strings.TrimSpace(*unbanUserID) != "" {
		resp, err := unbanSpaceMember(stream, uint32(*spaceID), *unbanUserID)
		if err != nil {
			log.Fatalf("unban space member: %v", err)
		}
		fmt.Printf("unbanned space member: space_id=%d target_user_id=%s name=%s\n", resp.SpaceId, resp.TargetUserId, resp.Space.Name)
	}

	if *listMembers || *listMembersAll {
		if *listMembersAll {
			afterID := *memberAfterID
			for {
				resp, err := listSpaceMembers(stream, uint32(*spaceID), afterID, uint32(*memberLimit))
				if err != nil {
					log.Fatalf("list space members: %v", err)
				}
				printMembers(resp.Members)
				if !resp.HasMore {
					break
				}
				if resp.NextAfterMemberId == 0 {
					break
				}
				fmt.Printf("next page cursor: after_member_id=%d\n", resp.NextAfterMemberId)
				afterID = resp.NextAfterMemberId
			}
		} else {
			resp, err := listSpaceMembers(stream, uint32(*spaceID), *memberAfterID, uint32(*memberLimit))
			if err != nil {
				log.Fatalf("list space members: %v", err)
			}
			printMembers(resp.Members)
			fmt.Printf("member page: space_id=%d after_member_id=%d next_after_member_id=%d has_more=%v\n", resp.SpaceId, *memberAfterID, resp.NextAfterMemberId, resp.HasMore)
		}
	}

	if err := setSpaceAllowChannelCreation(stream, uint32(*spaceID), *allowChannelCreation); err != nil {
		log.Fatalf("set channel creation: %v", err)
	}
	fmt.Printf("updated channel creation flag: space_id=%d allow_channel_creation=%v\n", *spaceID, *allowChannelCreation)

	var activeChannelID uint32
	if *createGroup {
		resp, err := createGroupChannel(stream, uint32(*spaceID), *groupName, *groupDescription)
		if err != nil {
			log.Fatalf("create group: %v", err)
		}
		activeChannelID = resp.ChannelId
		fmt.Printf("created group: space_id=%d channel_id=%d name=%s\n", resp.SpaceId, resp.ChannelId, resp.Channel.Name)
	}

	if activeChannelID == 0 {
		activeChannelID = uint32(*channelID)
	}

	autoDeleteChannelID := uint32(*groupAutoDeleteChannelID)
	if autoDeleteChannelID == 0 {
		autoDeleteChannelID = activeChannelID
	}
	if autoDeleteChannelID == 0 {
		autoDeleteChannelID = uint32(*channelID)
	}
	if autoDeleteChannelID != 0 && *groupAutoDeleteAfterSeconds > 0 {
		resp, err := setGroupAutoDelete(stream, autoDeleteChannelID, uint32(*groupAutoDeleteAfterSeconds))
		if err != nil {
			log.Fatalf("set group auto delete: %v", err)
		}
		fmt.Printf(
			"updated group auto delete: channel_id=%d auto_delete_after_seconds=%d name=%s\n",
			resp.ChannelId,
			resp.AutoDeleteAfterSeconds,
			resp.Channel.Name,
		)
		if activeChannelID == 0 {
			activeChannelID = autoDeleteChannelID
		}
	}

	if activeChannelID == 0 {
		return
	}

	channels, err := listChannels(ctx, stream, uint32(*spaceID))
	if err != nil {
		log.Fatalf("list channels: %v", err)
	}
	printChannels(channels)

	if err := subscribeChannel(stream, activeChannelID, *syncAfterSeq); err != nil {
		log.Fatalf("subscribe channel: %v", err)
	}
	fmt.Printf("subscribed: channel_id=%d\n", activeChannelID)

	if _, _, _, err := syncChannel(stream, activeChannelID, *syncAfterSeq, 50); err != nil {
		log.Fatalf("sync channel: %v", err)
	}
	fmt.Printf("synced channel: channel_id=%d from_seq=%d\n", activeChannelID, *syncAfterSeq)

	if strings.TrimSpace(*messageText) != "" {
		ack, err := sendTextMessage(stream, activeChannelID, *messageText)
		if err != nil {
			log.Fatalf("send message: %v", err)
		}
		fmt.Printf("message ack: message_id=%s seq=%d client_msg_id=%s\n", ack.MessageId, ack.Seq, ack.ClientMsgId)
	}
}

func authenticate(ctx context.Context, stream network.Stream, privKey corecrypto.PrivKey, clientPeerID string, clientAgent string, protocolID string) (*sessionv1.AuthResult, error) {
	reqID := requestID("hello")
	if err := writeEnvelope(stream, sessionv1.MsgType_HELLO, reqID, &sessionv1.Hello{
		ClientPeerId:    clientPeerID,
		ClientAgent:     clientAgent,
		ProtocolVersion: "1.0.0",
	}); err != nil {
		return nil, err
	}

	env, err := readEnvelopeExpect(stream, sessionv1.MsgType_AUTH_CHALLENGE)
	if err != nil {
		return nil, err
	}

	var challengeMsg sessionv1.AuthChallenge
	if err := protocol.UnmarshalBody(env.Body, &challengeMsg); err != nil {
		return nil, fmt.Errorf("unmarshal auth challenge: %w", err)
	}

	payload := auth.BuildChallengePayload(protocolID, &auth.Challenge{
		ClientPeerID: clientPeerID,
		NodePeerID:   challengeMsg.NodePeerId,
		Nonce:        challengeMsg.Nonce,
		IssuedAt:     time.UnixMilli(int64(challengeMsg.IssuedAtMs)).UTC(),
		ExpiresAt:    time.UnixMilli(int64(challengeMsg.ExpiresAtMs)).UTC(),
	})
	signature, err := privKey.Sign(payload)
	if err != nil {
		return nil, fmt.Errorf("sign challenge: %w", err)
	}
	publicKey, err := corecrypto.MarshalPublicKey(privKey.GetPublic())
	if err != nil {
		return nil, fmt.Errorf("marshal public key: %w", err)
	}

	if err := writeEnvelope(stream, sessionv1.MsgType_AUTH_PROVE, requestID("prove"), &sessionv1.AuthProve{
		ClientPeerId: clientPeerID,
		Nonce:        challengeMsg.Nonce,
		IssuedAtMs:   challengeMsg.IssuedAtMs,
		ExpiresAtMs:  challengeMsg.ExpiresAtMs,
		Signature:    signature,
		PublicKey:    publicKey,
	}); err != nil {
		return nil, err
	}

	authEnv, err := readEnvelopeExpect(stream, sessionv1.MsgType_AUTH_RESULT)
	if err != nil {
		return nil, err
	}

	var result sessionv1.AuthResult
	if err := protocol.UnmarshalBody(authEnv.Body, &result); err != nil {
		return nil, fmt.Errorf("unmarshal auth result: %w", err)
	}
	if !result.Ok {
		return nil, fmt.Errorf("authentication rejected")
	}
	return &result, nil
}

func setMemberRole(stream network.Stream, spaceID uint32, targetUserID string, role sessionv1.MemberRole) error {
	if err := writeEnvelope(stream, sessionv1.MsgType_ADMIN_SET_SPACE_MEMBER_ROLE_REQ, requestID("set-role"), &sessionv1.AdminSetSpaceMemberRoleReq{
		SpaceId:      spaceID,
		TargetUserId: targetUserID,
		Role:         role,
	}); err != nil {
		return err
	}
	env, err := readEnvelopeExpect(stream, sessionv1.MsgType_ADMIN_SET_SPACE_MEMBER_ROLE_RESP)
	if err != nil {
		return err
	}
	var resp sessionv1.AdminSetSpaceMemberRoleResp
	if err := protocol.UnmarshalBody(env.Body, &resp); err != nil {
		return fmt.Errorf("unmarshal set member role resp: %w", err)
	}
	if !resp.Ok {
		return fmt.Errorf("set member role rejected")
	}
	return nil
}

func setSpaceAllowChannelCreation(stream network.Stream, spaceID uint32, enabled bool) error {
	if err := writeEnvelope(stream, sessionv1.MsgType_ADMIN_SET_SPACE_CHANNEL_CREATION_REQ, requestID("set-create"), &sessionv1.AdminSetSpaceChannelCreationReq{
		SpaceId:              spaceID,
		AllowChannelCreation: enabled,
	}); err != nil {
		return err
	}
	env, err := readEnvelopeExpect(stream, sessionv1.MsgType_ADMIN_SET_SPACE_CHANNEL_CREATION_RESP)
	if err != nil {
		return err
	}
	var resp sessionv1.AdminSetSpaceChannelCreationResp
	if err := protocol.UnmarshalBody(env.Body, &resp); err != nil {
		return fmt.Errorf("unmarshal set channel creation resp: %w", err)
	}
	if !resp.Ok {
		return fmt.Errorf("set channel creation rejected")
	}
	return nil
}

func createGroupChannel(stream network.Stream, spaceID uint32, name string, description string) (*sessionv1.CreateGroupResp, error) {
	if err := writeEnvelope(stream, sessionv1.MsgType_CREATE_GROUP_REQ, requestID("create-group"), &sessionv1.CreateGroupReq{
		SpaceId:         spaceID,
		Name:            name,
		Description:     description,
		Visibility:      sessionv1.Visibility_PUBLIC,
		SlowModeSeconds: 0,
	}); err != nil {
		return nil, err
	}
	env, err := readEnvelopeExpect(stream, sessionv1.MsgType_CREATE_GROUP_RESP)
	if err != nil {
		return nil, err
	}
	var resp sessionv1.CreateGroupResp
	if err := protocol.UnmarshalBody(env.Body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal create group resp: %w", err)
	}
	if !resp.Ok {
		return nil, fmt.Errorf("create group rejected")
	}
	return &resp, nil
}

func setGroupAutoDelete(stream network.Stream, channelID uint32, seconds uint32) (*sessionv1.AdminSetGroupAutoDeleteResp, error) {
	if err := writeEnvelope(stream, sessionv1.MsgType_ADMIN_SET_GROUP_AUTO_DELETE_REQ, requestID("group-auto-delete"), &sessionv1.AdminSetGroupAutoDeleteReq{
		ChannelId:              channelID,
		AutoDeleteAfterSeconds: seconds,
	}); err != nil {
		return nil, err
	}
	env, err := readEnvelopeExpect(stream, sessionv1.MsgType_ADMIN_SET_GROUP_AUTO_DELETE_RESP)
	if err != nil {
		return nil, err
	}
	var resp sessionv1.AdminSetGroupAutoDeleteResp
	if err := protocol.UnmarshalBody(env.Body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal group auto delete resp: %w", err)
	}
	if !resp.Ok {
		return nil, fmt.Errorf("set group auto delete rejected")
	}
	return &resp, nil
}

func joinSpace(stream network.Stream, spaceID uint32) (*sessionv1.JoinSpaceResp, error) {
	if err := writeEnvelope(stream, sessionv1.MsgType_JOIN_SPACE_REQ, requestID("join"), &sessionv1.JoinSpaceReq{
		SpaceId: spaceID,
	}); err != nil {
		return nil, err
	}
	env, err := readEnvelopeExpect(stream, sessionv1.MsgType_JOIN_SPACE_RESP)
	if err != nil {
		return nil, err
	}
	var resp sessionv1.JoinSpaceResp
	if err := protocol.UnmarshalBody(env.Body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal join space resp: %w", err)
	}
	if !resp.Ok {
		return nil, fmt.Errorf("join space rejected")
	}
	return &resp, nil
}

func inviteSpaceMember(stream network.Stream, spaceID uint32, targetUserID string) (*sessionv1.InviteSpaceMemberResp, error) {
	if err := writeEnvelope(stream, sessionv1.MsgType_INVITE_SPACE_MEMBER_REQ, requestID("invite"), &sessionv1.InviteSpaceMemberReq{
		SpaceId:      spaceID,
		TargetUserId: targetUserID,
	}); err != nil {
		return nil, err
	}
	env, err := readEnvelopeExpect(stream, sessionv1.MsgType_INVITE_SPACE_MEMBER_RESP)
	if err != nil {
		return nil, err
	}
	var resp sessionv1.InviteSpaceMemberResp
	if err := protocol.UnmarshalBody(env.Body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal invite space member resp: %w", err)
	}
	if !resp.Ok {
		return nil, fmt.Errorf("invite space member rejected")
	}
	return &resp, nil
}

func kickSpaceMember(stream network.Stream, spaceID uint32, targetUserID string) (*sessionv1.KickSpaceMemberResp, error) {
	if err := writeEnvelope(stream, sessionv1.MsgType_KICK_SPACE_MEMBER_REQ, requestID("kick"), &sessionv1.KickSpaceMemberReq{
		SpaceId:      spaceID,
		TargetUserId: targetUserID,
	}); err != nil {
		return nil, err
	}
	env, err := readEnvelopeExpect(stream, sessionv1.MsgType_KICK_SPACE_MEMBER_RESP)
	if err != nil {
		return nil, err
	}
	var resp sessionv1.KickSpaceMemberResp
	if err := protocol.UnmarshalBody(env.Body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal kick space member resp: %w", err)
	}
	if !resp.Ok {
		return nil, fmt.Errorf("kick space member rejected")
	}
	return &resp, nil
}

func banSpaceMember(stream network.Stream, spaceID uint32, targetUserID string) (*sessionv1.BanSpaceMemberResp, error) {
	if err := writeEnvelope(stream, sessionv1.MsgType_BAN_SPACE_MEMBER_REQ, requestID("ban"), &sessionv1.BanSpaceMemberReq{
		SpaceId:      spaceID,
		TargetUserId: targetUserID,
	}); err != nil {
		return nil, err
	}
	env, err := readEnvelopeExpect(stream, sessionv1.MsgType_BAN_SPACE_MEMBER_RESP)
	if err != nil {
		return nil, err
	}
	var resp sessionv1.BanSpaceMemberResp
	if err := protocol.UnmarshalBody(env.Body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal ban space member resp: %w", err)
	}
	if !resp.Ok {
		return nil, fmt.Errorf("ban space member rejected")
	}
	return &resp, nil
}

func unbanSpaceMember(stream network.Stream, spaceID uint32, targetUserID string) (*sessionv1.UnbanSpaceMemberResp, error) {
	if err := writeEnvelope(stream, sessionv1.MsgType_UNBAN_SPACE_MEMBER_REQ, requestID("unban"), &sessionv1.UnbanSpaceMemberReq{
		SpaceId:      spaceID,
		TargetUserId: targetUserID,
	}); err != nil {
		return nil, err
	}
	env, err := readEnvelopeExpect(stream, sessionv1.MsgType_UNBAN_SPACE_MEMBER_RESP)
	if err != nil {
		return nil, err
	}
	var resp sessionv1.UnbanSpaceMemberResp
	if err := protocol.UnmarshalBody(env.Body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal unban space member resp: %w", err)
	}
	if !resp.Ok {
		return nil, fmt.Errorf("unban space member rejected")
	}
	return &resp, nil
}

func listSpaces(stream network.Stream) ([]*sessionv1.SpaceSummary, error) {
	if err := writeEnvelope(stream, sessionv1.MsgType_LIST_SPACES_REQ, requestID("list-spaces"), &sessionv1.ListSpacesReq{}); err != nil {
		return nil, err
	}
	env, err := readEnvelopeExpect(stream, sessionv1.MsgType_LIST_SPACES_RESP)
	if err != nil {
		return nil, err
	}
	var resp sessionv1.ListSpacesResp
	if err := protocol.UnmarshalBody(env.Body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal list spaces resp: %w", err)
	}
	return resp.Spaces, nil
}

func listChannels(ctx context.Context, stream network.Stream, spaceID uint32) ([]*sessionv1.ChannelSummary, error) {
	_ = ctx
	if err := writeEnvelope(stream, sessionv1.MsgType_LIST_CHANNELS_REQ, requestID("list-channels"), &sessionv1.ListChannelsReq{
		SpaceId: spaceID,
	}); err != nil {
		return nil, err
	}
	env, err := readEnvelopeExpect(stream, sessionv1.MsgType_LIST_CHANNELS_RESP)
	if err != nil {
		return nil, err
	}
	var resp sessionv1.ListChannelsResp
	if err := protocol.UnmarshalBody(env.Body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal list channels resp: %w", err)
	}
	return resp.Channels, nil
}

func listSpaceMembers(stream network.Stream, spaceID uint32, afterMemberID uint64, limit uint32) (*sessionv1.ListSpaceMembersResp, error) {
	if err := writeEnvelope(stream, sessionv1.MsgType_LIST_SPACE_MEMBERS_REQ, requestID("list-members"), &sessionv1.ListSpaceMembersReq{
		SpaceId:       spaceID,
		AfterMemberId: afterMemberID,
		Limit:         limit,
	}); err != nil {
		return nil, err
	}
	env, err := readEnvelopeExpect(stream, sessionv1.MsgType_LIST_SPACE_MEMBERS_RESP)
	if err != nil {
		return nil, err
	}
	var resp sessionv1.ListSpaceMembersResp
	if err := protocol.UnmarshalBody(env.Body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal list space members resp: %w", err)
	}
	return &resp, nil
}

func printSpaces(spaces []*sessionv1.SpaceSummary) {
	fmt.Println("spaces:")
	for _, srv := range spaces {
		fmt.Printf("  - %d (%s) allow_channel_creation=%v members=%d\n", srv.SpaceId, srv.Name, srv.AllowChannelCreation, srv.MemberCount)
	}
}

func getCreateSpacePermissions(stream network.Stream) (*sessionv1.GetCreateSpacePermissionsResp, error) {
	if err := writeEnvelope(stream, sessionv1.MsgType_GET_CREATE_SPACE_PERMISSIONS_REQ, requestID("server-perms"), &sessionv1.GetCreateSpacePermissionsReq{}); err != nil {
		return nil, err
	}
	env, err := readEnvelopeExpect(stream, sessionv1.MsgType_GET_CREATE_SPACE_PERMISSIONS_RESP)
	if err != nil {
		return nil, err
	}
	var resp sessionv1.GetCreateSpacePermissionsResp
	if err := protocol.UnmarshalBody(env.Body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal get create-space permissions resp: %w", err)
	}
	if !resp.Ok {
		return nil, fmt.Errorf("get create-space permissions rejected")
	}
	return &resp, nil
}

func getSpacePermissions(stream network.Stream, spaceID uint32) (*sessionv1.GetCreateGroupPermissionsResp, error) {
	if err := writeEnvelope(stream, sessionv1.MsgType_GET_CREATE_GROUP_PERMISSIONS_REQ, requestID("space-perms"), &sessionv1.GetCreateGroupPermissionsReq{
		SpaceId: spaceID,
	}); err != nil {
		return nil, err
	}
	env, err := readEnvelopeExpect(stream, sessionv1.MsgType_GET_CREATE_GROUP_PERMISSIONS_RESP)
	if err != nil {
		return nil, err
	}
	var resp sessionv1.GetCreateGroupPermissionsResp
	if err := protocol.UnmarshalBody(env.Body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal get create-group permissions resp: %w", err)
	}
	if !resp.Ok {
		return nil, fmt.Errorf("get create-group permissions rejected")
	}
	return &resp, nil
}

func createSpaceRPC(stream network.Stream, name string, description string, visibility sessionv1.Visibility, allowChannelCreation bool) (*sessionv1.CreateSpaceResp, error) {
	if err := writeEnvelope(stream, sessionv1.MsgType_CREATE_SPACE_REQ, requestID("create-space"), &sessionv1.CreateSpaceReq{
		Name:                 name,
		Description:          description,
		Visibility:           visibility,
		AllowChannelCreation: allowChannelCreation,
	}); err != nil {
		return nil, err
	}
	env, err := readEnvelopeExpect(stream, sessionv1.MsgType_CREATE_SPACE_RESP)
	if err != nil {
		return nil, err
	}
	var resp sessionv1.CreateSpaceResp
	if err := protocol.UnmarshalBody(env.Body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal create space resp: %w", err)
	}
	if !resp.Ok {
		return nil, fmt.Errorf("create space rejected")
	}
	return &resp, nil
}

func printChannels(channels []*sessionv1.ChannelSummary) {
	fmt.Println("channels:")
	for _, ch := range channels {
		fmt.Printf("  - %d (%s) type=%s auto_delete_after_seconds=%d can_view=%v can_send_message=%v\n", ch.ChannelId, ch.Name, ch.Type.String(), ch.AutoDeleteAfterSeconds, ch.CanView, ch.CanSendMessage)
	}
}

func printMembers(members []*sessionv1.SpaceMemberSummary) {
	fmt.Println("members:")
	for _, m := range members {
		fmt.Printf("  - member_id=%d user_id=%s display_name=%s role=%s banned=%v\n", m.MemberId, m.UserId, m.DisplayName, m.Role.String(), m.IsBanned)
	}
}

func subscribeChannel(stream network.Stream, channelID uint32, lastSeenSeq uint64) error {
	if err := writeEnvelope(stream, sessionv1.MsgType_SUBSCRIBE_CHANNEL_REQ, requestID("subscribe"), &sessionv1.SubscribeChannelReq{
		ChannelId:   channelID,
		LastSeenSeq: lastSeenSeq,
	}); err != nil {
		return err
	}
	env, err := readEnvelopeExpect(stream, sessionv1.MsgType_SUBSCRIBE_CHANNEL_RESP)
	if err != nil {
		return err
	}
	var resp sessionv1.SubscribeChannelResp
	if err := protocol.UnmarshalBody(env.Body, &resp); err != nil {
		return fmt.Errorf("unmarshal subscribe resp: %w", err)
	}
	if !resp.Ok {
		return fmt.Errorf("subscribe rejected")
	}
	return nil
}

func syncChannel(stream network.Stream, channelID uint32, afterSeq uint64, limit uint32) ([]*sessionv1.MessageEvent, uint64, bool, error) {
	if err := writeEnvelope(stream, sessionv1.MsgType_SYNC_CHANNEL_REQ, requestID("sync"), &sessionv1.SyncChannelReq{
		ChannelId: channelID,
		AfterSeq:  afterSeq,
		Limit:     limit,
	}); err != nil {
		return nil, 0, false, err
	}
	env, err := readEnvelopeExpect(stream, sessionv1.MsgType_SYNC_CHANNEL_RESP)
	if err != nil {
		return nil, 0, false, err
	}
	var resp sessionv1.SyncChannelResp
	if err := protocol.UnmarshalBody(env.Body, &resp); err != nil {
		return nil, 0, false, fmt.Errorf("unmarshal sync resp: %w", err)
	}
	return resp.Messages, resp.NextAfterSeq, resp.HasMore, nil
}

func sendTextMessage(stream network.Stream, channelID uint32, text string) (*sessionv1.SendMessageAck, error) {
	reqID := requestID("send")
	if err := writeEnvelope(stream, sessionv1.MsgType_SEND_MESSAGE_REQ, reqID, &sessionv1.SendMessageReq{
		ChannelId:   channelID,
		ClientMsgId: requestID("client"),
		MessageType: sessionv1.MessageType_TEXT,
		Content: &sessionv1.MessageContent{
			Text: text,
		},
	}); err != nil {
		return nil, err
	}
	env, err := readEnvelopeExpect(stream, sessionv1.MsgType_SEND_MESSAGE_ACK)
	if err != nil {
		return nil, err
	}
	var resp sessionv1.SendMessageAck
	if err := protocol.UnmarshalBody(env.Body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal send ack: %w", err)
	}
	if !resp.Ok {
		return nil, fmt.Errorf("message rejected")
	}
	return &resp, nil
}

func parseMemberRole(value string) (sessionv1.MemberRole, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "owner":
		return sessionv1.MemberRole_OWNER, nil
	case "admin":
		return sessionv1.MemberRole_ADMIN, nil
	case "member":
		return sessionv1.MemberRole_MEMBER, nil
	case "subscriber":
		return sessionv1.MemberRole_SUBSCRIBER, nil
	default:
		return sessionv1.MemberRole_MEMBER_ROLE_UNSPECIFIED, fmt.Errorf("unsupported role %q", value)
	}
}

func parseVisibility(value string) (sessionv1.Visibility, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "public":
		return sessionv1.Visibility_PUBLIC, nil
	case "private":
		return sessionv1.Visibility_PRIVATE, nil
	default:
		return sessionv1.Visibility_VISIBILITY_UNSPECIFIED, fmt.Errorf("unsupported visibility %q", value)
	}
}

func requestID(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}

func writeEnvelope(stream network.Stream, msgType sessionv1.MsgType, requestID string, body gproto.Message) error {
	return protocol.WriteEnvelope(stream, mustEnvelope(msgType, requestID, body))
}

func mustEnvelope(msgType sessionv1.MsgType, requestID string, body gproto.Message) *sessionv1.Envelope {
	env, err := protocol.NewEnvelope(msgType, requestID, body)
	if err != nil {
		panic(err)
	}
	return env
}

func readEnvelopeExpect(stream network.Stream, want sessionv1.MsgType) (*sessionv1.Envelope, error) {
	env, err := protocol.ReadEnvelope(stream)
	if err != nil {
		return nil, err
	}
	if env.MsgType != want {
		return nil, fmt.Errorf("unexpected msg type %s, want %s", env.MsgType.String(), want.String())
	}
	return env, nil
}

func addrInfo(raw string) (*peer.AddrInfo, error) {
	maddr, err := ma.NewMultiaddr(raw)
	if err != nil {
		return nil, err
	}
	info, err := peer.AddrInfoFromP2pAddr(maddr)
	if err != nil {
		return nil, err
	}
	return info, nil
}

func parseBootstrapPeers(values bootstrapPeerFlags) ([]peer.AddrInfo, error) {
	peers := make([]peer.AddrInfo, 0, len(values)+len(dht.GetDefaultBootstrapPeerAddrInfos()))
	for _, addrInfo := range dht.GetDefaultBootstrapPeerAddrInfos() {
		peers = append(peers, addrInfo)
	}
	for _, raw := range values {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		info, err := addrInfo(raw)
		if err != nil {
			return nil, fmt.Errorf("parse bootstrap peer %q: %w", raw, err)
		}
		peers = append(peers, *info)
	}
	return peers, nil
}

func connectPeer(ctx context.Context, host corehost.Host, info peer.AddrInfo) error {
	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return host.Connect(connectCtx, info)
}

func discoverMeshserverPeer(ctx context.Context, host corehost.Host, node *dht.IpfsDHT, namespace string) (*peer.AddrInfo, error) {
	discovery := discoveryrouting.NewRoutingDiscovery(node)
	peerChan, err := discovery.FindPeers(ctx, namespace)
	if err != nil {
		return nil, fmt.Errorf("start discovery: %w", err)
	}

	for info := range peerChan {
		if info.ID == "" || info.ID == host.ID() {
			continue
		}
		if err := connectPeer(ctx, host, info); err != nil {
			log.Printf("connect discovered peer %s failed: %v", info.ID.String(), err)
			continue
		}
		return &info, nil
	}

	return nil, fmt.Errorf("no meshserver peer discovered in namespace %q", namespace)
}

type joinSpaceFlags []uint64

func (f *joinSpaceFlags) String() string {
	items := make([]string, 0, len(*f))
	for _, item := range *f {
		items = append(items, fmt.Sprintf("%d", item))
	}
	return strings.Join(items, ",")
}

func (f *joinSpaceFlags) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	var parsed uint64
	if _, err := fmt.Sscan(value, &parsed); err != nil {
		return fmt.Errorf("parse space id %q: %w", value, err)
	}
	*f = append(*f, parsed)
	return nil
}

type bootstrapPeerFlags []string

func (f *bootstrapPeerFlags) String() string {
	return strings.Join(*f, ",")
}

func (f *bootstrapPeerFlags) Set(value string) error {
	*f = append(*f, value)
	return nil
}
