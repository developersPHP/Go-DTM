package rabbitmq_dirver

import (
	"fmt"
	"github.com/pkg/errors"
	"github.com/streadway/amqp"
	"sync"
)

var (
	ErrColse = errors.New("pool is closed")
)

type PoolConn struct {
	Conn     *amqp.Connection
	mu       sync.RWMutex
	c        *ChannelPool
	unsuable bool
}

func (p *PoolConn) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.unsuable {
		if p.Conn != nil {
			p.Conn.Close()
		}
		return nil
	}
	//把原来的连接返回池子
	return p.c.Put(p.Conn)

}

func (p *PoolConn) MarkUnusable() {
	p.mu.Lock()
	p.unsuable = true
	p.mu.Unlock()
}

func (c *ChannelPool) WarpConn(conn *amqp.Connection) *PoolConn {
	p := &PoolConn{c: c}
	p.Conn = conn
	return p
}

type Factory func() (*amqp.Connection, error)
type ChannelPool struct {
	mu      sync.RWMutex
	Conns   chan *amqp.Connection
	factory Factory
}

func NewChannelPool(initialCap, maxCap int, factory Factory) (*ChannelPool, error) {
	if initialCap < 0 || maxCap < 0 || initialCap > maxCap {
		return nil, errors.New("invalid capacity settings")
	}
	c := &ChannelPool{Conns: make(chan *amqp.Connection, maxCap), factory: factory}
	for i := 0; i < initialCap; i++ {
		conn, err := factory()
		fmt.Println("创建的句柄", conn)
		if err != nil {
			c.Close()
			return nil, fmt.Errorf("factory is not able to fill the pool:%s", err)
		}
		//这里是往池子里塞连接
		c.Conns <- conn
	}
	fmt.Println("创建池子", *c)
	return c, nil
}

func (c *ChannelPool) Close() {
	c.mu.Lock()
	conns := c.Conns
	c.Conns = nil
	c.factory = nil
	c.mu.Unlock()

	if conns == nil {
		return
	}
	close(conns)
	for conn := range conns {
		conn.Close()
	}

}
func (c *ChannelPool) Get() (*PoolConn, error) {
	conns, factory := c.getConnsAndFactory()
	//如果获取到的连接池为空，则返回错误
	if conns == nil {
		return nil, ErrColse
	}
	select {
	//向池子取连接
	case conn := <-conns:
		if conn == nil {
			return nil, ErrColse
		}
		return c.WarpConn(conn), nil
	default:
		conn, err := factory()
		if err != nil {
			return nil, err
		}
		return c.WarpConn(conn), nil
	}
}

func (c *ChannelPool) Put(conn *amqp.Connection) error {
	if conn == nil {
		return errors.New("connection is nil,rejecting")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	//池子为空，池子没有初始化，把连接关闭
	if c.Conns == nil {
		return conn.Close()
	}
	select {
	case c.Conns <- conn:
		return nil
	default:
		//池子满了，会走这里，把连接关闭
		return conn.Close()
	}
}

func (c *ChannelPool) getConnsAndFactory() (chan *amqp.Connection, Factory) {
	c.mu.Lock()
	conn := c.Conns
	factory := c.factory
	c.mu.Unlock()
	return conn, factory
}

func (c *ChannelPool) Len() int {
	conns, _ := c.getConnsAndFactory()
	return len(conns)
}
