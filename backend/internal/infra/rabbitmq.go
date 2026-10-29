package infra

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// ---- RabbitMQ 适配器（EM_BROKER=rabbitmq 时启用）----
//
// 拓扑：topic 交换机 "evolvermon"（durable），订阅按 `em:{group}.{topic}` 声明
// durable 队列并以 topic 作为绑定键（支持 workorder.* 通配订阅）。
// 投递语义：手动 ack；消费失败内联重试 3 次后落日志丢弃（防毒消息死循环）。

const (
	rabbitExchange  = "evolvermon"
	rabbitRetryMax  = 3
	rabbitRedialGap = 3 * time.Second
)

type rabbitBroker struct {
	url    string
	mu     sync.Mutex // 保护 conn/pub 的建立与重连
	conn   *amqp.Connection
	pub    *amqp.Channel
	closed bool
	wg     sync.WaitGroup // 订阅协程
}

// newRabbitBroker 按可达性建立连接并声明交换机（启动即失败，fail-fast）。
func newRabbitBroker(url string) (Broker, error) {
	if _, err := parseRabbitURL(url); err != nil {
		return nil, err
	}
	b := &rabbitBroker{url: url}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.ensureLocked(); err != nil {
		return nil, err
	}
	return b, nil
}

func parseRabbitURL(url string) (string, error) {
	if url == "" {
		return "", errors.New("rabbitmq: EM_BROKER_URL is required (amqp://user:pass@host:5672/vhost)")
	}
	return url, nil
}

func (b *rabbitBroker) Name() string { return "rabbitmq" }

// ensureLocked 重建连接与发布通道（调用方持锁）。
func (b *rabbitBroker) ensureLocked() error {
	if b.closed {
		return errors.New("rabbitmq: broker closed")
	}
	if b.conn != nil && !b.conn.IsClosed() && b.pub != nil && !b.pub.IsClosed() {
		return nil
	}
	if b.conn != nil && !b.conn.IsClosed() {
		_ = b.conn.Close() // 通道失效则整体重建
	}
	conn, err := amqp.Dial(b.url)
	if err != nil {
		return fmt.Errorf("rabbitmq dial: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("rabbitmq channel: %w", err)
	}
	if err := ch.ExchangeDeclare(rabbitExchange, "topic", true, false, false, false, nil); err != nil {
		_ = conn.Close()
		return fmt.Errorf("rabbitmq exchange declare: %w", err)
	}
	b.conn, b.pub = conn, ch
	return nil
}

func (b *rabbitBroker) Publish(ctx context.Context, m Message) error {
	key := m.Key
	if key == "" {
		key = m.Event
	}
	body, err := marshalMessage(m)
	if err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for attempt := 0; attempt < 2; attempt++ { // 失败重连一次再试
		if err := b.ensureLocked(); err != nil {
			return err
		}
		err = b.pub.PublishWithContext(ctx, rabbitExchange, key, false, false, amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			MessageId:    m.ID,
			Timestamp:    m.Timestamp,
			Body:         body,
			Headers: amqp.Table{
				"tenantId": m.TenantID,
				"event":    m.Event,
				"topic":    m.Topic,
			},
		})
		if err == nil {
			return nil
		}
		log.Printf("[infra:rabbitmq] publish %s attempt %d: %v", m.Event, attempt+1, err)
		_ = b.conn.Close() // 强制下次 ensure 重建
		b.conn, b.pub = nil, nil
	}
	return err
}

// Subscribe 非阻塞注册消费者；连接断开后自动重连并重新声明队列。
func (b *rabbitBroker) Subscribe(ctx context.Context, topic, group string, h Handler) error {
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		for {
			if ctx.Err() != nil {
				return
			}
			err := b.consumeOnce(ctx, topic, group, h)
			if ctx.Err() != nil || b.isClosed() {
				return
			}
			log.Printf("[infra:rabbitmq] consume %s/%s interrupted: %v, redial in %s", group, topic, err, rabbitRedialGap)
			select {
			case <-time.After(rabbitRedialGap):
			case <-ctx.Done():
				return
			}
		}
	}()
	return nil
}

func (b *rabbitBroker) isClosed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closed
}

func (b *rabbitBroker) consumeOnce(ctx context.Context, topic, group string, h Handler) error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return errors.New("rabbitmq: broker closed")
	}
	if err := b.ensureLocked(); err != nil {
		b.mu.Unlock()
		return err
	}
	conn := b.conn
	b.mu.Unlock()

	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()
	if err := ch.Qos(16, 0, false); err != nil {
		return err
	}
	queue := "em." + group + "." + topic
	if _, err := ch.QueueDeclare(queue, true, false, false, false, nil); err != nil {
		return err
	}
	if err := ch.QueueBind(queue, topic, rabbitExchange, false, nil); err != nil {
		return err
	}
	deliveries, err := ch.ConsumeWithContext(ctx, queue, "em-"+group, false, false, false, false, nil)
	if err != nil {
		return err
	}
	chClose := ch.NotifyClose(make(chan *amqp.Error, 1))
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-chClose:
			return fmt.Errorf("channel closed: %v", err)
		case d, ok := <-deliveries:
			if !ok {
				return errors.New("deliveries closed")
			}
			m, err := unmarshalMessage(d.Body)
			if err != nil {
				log.Printf("[infra:rabbitmq] bad message %s: %v, drop", d.MessageId, err)
				_ = d.Ack(false)
				continue
			}
			var herr error
			for i := 1; i <= rabbitRetryMax; i++ {
				if herr = h(ctx, m); herr == nil {
					break
				}
				time.Sleep(time.Duration(i) * 200 * time.Millisecond)
			}
			if herr != nil {
				log.Printf("[infra:rabbitmq] handler %s failed after %d tries: %v, drop", m.Event, rabbitRetryMax, herr)
			}
			_ = d.Ack(false)
		}
	}
}

func (b *rabbitBroker) Close() error {
	b.mu.Lock()
	b.closed = true
	if b.conn != nil && !b.conn.IsClosed() {
		_ = b.conn.Close()
	}
	b.mu.Unlock()
	b.wg.Wait()
	return nil
}
