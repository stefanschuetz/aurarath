package service

import (
	"github.com/joernweissenborn/aurarath/network/node"
	"github.com/joernweissenborn/eventual2go"
	"github.com/joernweissenborn/aurarath/config"
	"github.com/joernweissenborn/aurarath/network/connection"
	"encoding/binary"
	"strings"
	"log"
	"github.com/joernweissenborn/aurarath/network/peer"
	"github.com/joernweissenborn/aurarath/messages"
	"sync"
	"fmt"
	"github.com/joernweissenborn/aurarath/appdescriptor"
)


type Service struct {

	uuid string

	r *eventual2go.Reactor

	appDescriptor *appdescriptor.AppDescriptor

	announcer *Announcer

	remove *eventual2go.Future

	incoming map[string]*connection.Incoming

	in eventual2go.StreamController

	peers map[string]*peer.Peer

	servicetype string

	codecs []byte

	logger *log.Logger

	connected *eventual2go.Future
	disconnected *eventual2go.Future

	newpeers eventual2go.StreamController
	gonepeers eventual2go.StreamController
}

func NewService(a *appdescriptor.AppDescriptor, servicetype string, cfg *config.Config, codecs []byte) (s *Service){
	s = new(Service)
	s.appDescriptor = a
	s.servicetype = servicetype
	s.codecs = codecs
	s.newpeers = eventual2go.NewStreamController()
	s.gonepeers = eventual2go.NewStreamController()

	s.incoming = map[string]*connection.Incoming{}
	s.in = eventual2go.NewStreamController()

	s.peers = map[string]*peer.Peer{}
	s.connected = eventual2go.NewFuture()
	s.disconnected = eventual2go.NewFuture()
	s.remove = eventual2go.NewFuture()

	s.r = eventual2go.NewReactor()
	s.r.React("service_arrived",s.serviceArrived)
	s.r.React("service_gone",s.serviceGone)
	s.r.React("service_greeted",s.serviceGreeted)
	s.r.AddStream(s.in.Where(messages.Is(messages.HELLO)))
	s.r.React("service_greeted_back",s.serviceGreetedBack)
	s.r.AddStream(s.in.Where(messages.Is(messages.HELLO_OK)))
	s.createIncoming(cfg)
	s.createAnnouncer()
	return
}

func (s *Service) UUID() string{
	return s.uuid
}

func (s *Service) Connected() *eventual2go.Future{
	return s.connected
}

func (s *Service) Disconnected() *eventual2go.Future{
	return s.disconnected
}

func (s *Service) Run() {
	s.announcer.Run()
}

func (s *Service) createIncoming(cfg *config.Config) {
	for _, addr := range cfg.NetworkInterfaces {
		s.logger.Println("Opening Incoming Socket on", addr)
		incoming, err := connection.NewIncoming(addr)
		if err == nil {
			s.in.Join(incoming.In().Where(messages.Valid).Transform(messages.ToIncomingMsg))
			s.incoming[addr] = incoming
		} else {
			s.logger.Println("Error opening socket",err)
		}
	}
}

func (s *Service) createAnnouncer() {
	addrs := []string{}

	for addr,i := range s.incoming {
		addrs = append(addrs,fmt.Sprintf("%s:%d",addr,i.Port()))
	}

	s.announcer = NewAnnouncer(s.uuid,addrs,s.servicetype,s.appDescriptor)
	s.r.AddStream("service_arrived",s.announcer.ServiceArrived())
	s.r.AddStream("service_gone",s.announcer.ServiceGone())
}

func (s *Service) serviceArrived(d eventual2go.Data) {
	sa := d.(ServiceArrived)
	if !s.peerExist(sa.UUID) {
		s.c
	}
}

func (s *Service) serviceGone(d eventual2go.Data) {
	s.m.Lock()
	defer  s.m.Unlock()
	r := d.(node.QueryResponseEvent)
	buf := strings.Split(r.Response.From, "@")
	if len(buf) != 2 {
		return
	}
	uuid := buf[0]
	ip := buf[1]

	if p, f:= s.peers[uuid];f{
		p.CloseConnection(ip)
	}
}

func (s *Service) peerLeave(d eventual2go.Data) {
	r := d.(node.LeaveEvent)
	s.logger.Println("Peerleave",d)
	buf := strings.Split(r.Name, "@")
	if len(buf) != 2 {
		return
	}
	uuid := buf[0]
	s.removePeer(uuid)
}

func (s *Service) createPeer(uuid string) (p *peer.Peer) {
	p = peer.New(uuid)
	p.Disconnected().Then(s.removePeer)
	s.newpeers.JoinFuture(p.Greeted())
	s.peers[uuid] = p
	return
}
func (s *Service) removePeer(d eventual2go.Data) (eventual2go.Data) {
	s.m.Lock()
	defer  s.m.Unlock()
	uuid := d.(string)
	s.logger.Println("Removing peer",uuid)
	delete(s.peers,uuid)
	s.gonepeers.Add(uuid)
	if len(s.peers) == 0 && !s.disconnected.IsComplete() {
		s.logger.Println("Disconnected")
		s.disconnected.Complete(nil)
	}
	return nil
}

func (s *Service) newPeer() {
	s.m.Lock()
	defer  s.m.Unlock()
	r := d.(node.QueryResponseEvent)
	buf := strings.Split(r.Response.From, "@")
	if len(buf) != 2 {
		return
	}

	uuid := buf[0]
	ip := buf[1]
	port := binary.LittleEndian.Uint16(r.Response.Payload)
	s.logger.Println("Found Peer ",uuid)

	p := s.peers[uuid]
	if p == nil {
		s.logger.Println("Peer does not exist, creating",uuid)
		p = s.createPeer(uuid)
		p.Connected().Then(s.greetPeer(r.Address))
	}

	p.OpenConnection(ip, port,s.UUID())
	s.logger.Println("DONE")

}


func (s *Service) greetPeer(iface string) eventual2go.CompletionHandler {
	return func(d eventual2go.Data) eventual2go.Data {
		p := d.(*peer.Peer)
		s.logger.Println("Greeting peer",p.Uuid())
		port := s.incoming[iface].Port()
		p.Send(messages.Flatten(&messages.Hello{s.getDetails(),iface,int(port)}))
		return nil
	}
}

func (s *Service) greetPeerBack() eventual2go.CompletionHandler {
	return func(d eventual2go.Data) eventual2go.Data {
		p := d.(*peer.Peer)
		s.logger.Println("Greeting peer back",p.Uuid())
		p.Send(messages.Flatten(&messages.HelloOk{s.getDetails()}))
		return nil
	}
}

func (s *Service) peerGreeted(d eventual2go.Data) {
	s.m.Lock()
	defer  s.m.Unlock()
	m := d.(messages.IncomingMessage)
	h := m.Msg.(*messages.Hello)
	s.logger.Println("Got greeting from ",m.Sender)
	p := s.peers[m.Sender]
	if p == nil {
		s.logger.Println("Peer does not exist, creating",m.Sender)
		p = s.createPeer(m.Sender)
		p.OpenConnection(h.Address, uint16(h.Port),s.UUID())
	}
	p.Connected().Then(s.greetPeerBack())
	p.SetDetails(h.PeerDetails)

	if !s.connected.IsComplete() {
		s.logger.Println("Connected")
		s.connected.Complete(m.Sender)
	}
}

func (s *Service) peerGreetedBack(d eventual2go.Data) {
	s.m.Lock()
	defer  s.m.Unlock()
	m := d.(messages.IncomingMessage)
	h := m.Msg.(*messages.HelloOk)
	s.logger.Println("Peer greeted back",m.Sender)
	p := s.peers[m.Sender]
	p.SetDetails(h.PeerDetails)
	if !s.connected.IsComplete() {
		s.connected.Complete(m.Sender)
	}
}

func (s *Service) peerExist(uuid string) (e bool){
	_,e = s.peers[uuid]
	return
}
func (s *Service) getPeer(uuid string) (p *peer.Peer){
	return s.peers[uuid]
}

func (s *Service) getDetails() peer.Details{
	return peer.Details{s.codecs}
}

func (s *Service) getConnectedPeers() (peers []*peer.Peer){
	s.m.RLock()
	defer  s.m.RUnlock()
	peers = []*peer.Peer{}
	for _,p := range s.peers{
		if p.Connected().IsComplete() && p.Greeted().IsComplete() {
			peers = append(peers,p)
		}
	}
	return
}

func (s *Service) replyToService(d eventual2go.Data){
	q := d.(node.QueryEvent)
	s.logger.Println("Found Service on",q.Address)
	if conn, f := s.incoming[q.Address];f {
		repl := make([]byte,2)
		binary.LittleEndian.PutUint16(repl,conn.Port())
		q.Query.Respond(repl)

	}
}

func (s *Service) Remove() {
	s.logger.Println("Stopping Service",s.UUID())
	s.node.Shutdown()
	for _, i := range s.incoming {
		i.Close()
	}
	for _, p := range s.peers {
		p.CloseAllConnections()
	}

	s.remove.Complete(nil)
	s.logger.Println("Service Stopped",s.UUID())
}

