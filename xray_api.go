package main

import (
	"context"
	"fmt"
	"time"

	"github.com/xtls/xray-core/app/proxyman/command"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/proxy/shadowsocks"
	"github.com/xtls/xray-core/proxy/trojan"
	"github.com/xtls/xray-core/proxy/vless"
	"github.com/xtls/xray-core/proxy/vmess"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func getHandlerClient() (command.HandlerServiceClient, *grpc.ClientConn, error) {
	conn, err := grpc.NewClient(
		xrayAPIAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, nil, err
	}
	return command.NewHandlerServiceClient(conn), conn, nil
}

func buildAccount(in Inbound, c Client) (*serial.TypedMessage, error) {
	switch in.Protocol {
	case "vless":
		acc := &vless.Account{Id: c.UUID}
		if in.Network == "tcp" && (in.Security == "tls" || in.Security == "reality") {
			flow := c.Flow
			if flow == "" {
				flow = "xtls-rprx-vision"
			}
			acc.Flow = flow
		}
		return serial.ToTypedMessage(acc), nil
	case "vmess":
		return serial.ToTypedMessage(&vmess.Account{Id: c.UUID}), nil
	case "trojan":
		return serial.ToTypedMessage(&trojan.Account{Password: c.UUID}), nil
	case "shadowsocks":
		// ✅ Cipher → CipherType
		return serial.ToTypedMessage(&shadowsocks.Account{
			Password:   c.UUID,
			CipherType: shadowsocks.CipherType_AES_128_GCM,
		}), nil
	}
	return nil, fmt.Errorf("unsupported protocol: %s", in.Protocol)
}

func AddUserLive(in Inbound, c Client) error {
	client, conn, err := getHandlerClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	account, err := buildAccount(in, c)
	if err != nil {
		return err
	}

	user := &protocol.User{
		Email:   c.Email,
		Level:   0,
		Account: account, // ✅ حالا *serial.TypedMessage است
	}

	op := &command.AddUserOperation{User: user}
	// ✅ استفاده از serial.ToTypedMessage به جای anypb.New
	typedOp := serial.ToTypedMessage(op)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = client.AlterInbound(ctx, &command.AlterInboundRequest{
		Tag:       "inbound-" + itoa(in.ID),
		Operation: typedOp, // ✅ نوع مطابقت دارد
	})
	return err
}

func RemoveUserLive(in Inbound, email string) error {
	client, conn, err := getHandlerClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	op := &command.RemoveUserOperation{Email: email}
	typedOp := serial.ToTypedMessage(op)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = client.AlterInbound(ctx, &command.AlterInboundRequest{
		Tag:       "inbound-" + itoa(in.ID),
		Operation: typedOp,
	})
	return err
}