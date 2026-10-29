package infra

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	kafkago "github.com/segmentio/kafka-go"
)

// ---- Kafka 适配器（EM_BROKER=kafka 时启用）----
//
// 每主题一个 Writer（key 哈希分区，acks=all）；订阅按消费组消费，
// 处理成功才提交位点（at-least-once）；消费失败内联重试 3 次后落日志丢弃。

const (
	kafkaRetryMax  = 3
	kafkaRedialGap = 3 * time.Second
)

type kafkaBroker struct {
	addrs   []string
	mu      sync.Mutex
	writers map[string]*kafkago.Writer
	cancels []context.CancelFunc // 各订阅的取消函数
	closed  bool
	wg      sync.WaitGroup
}

// newKafkaBroker 解析地址并探测首个 broker 可达性（fail-fast）。
// URL 形如 kafka://host1:9092,host2:9092（kafka:// 前缀可省）。
func newKafkaBroker(url string) (Broker, error) {
	addrs, err := parseKafkaAddrs(url)
	if err != nil {
		return nil, err
	}
	dialer := &kafkago.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.Dial("tcp", addrs[0])
	if err != nil {
		return nil, fmt.Errorf("kafka dial %s: %w", addrs[0], err)
	}
	_ = conn.Close()
	return &kafkaBroker{addrs: addrs, writers: map[string]*kafkago.Writer{}}, nil
}

func parseKafkaAddrs(url string) ([]string, error) {
	url = strings.TrimPrefix(strings.TrimSpace(url), "kafka://")
	if url == "" {
		return nil, errors.New("kafka: EM_BROKER_URL is required (kafka://host1:9092,host2:9092)")
	}
	var addrs []string
	for _, a := range strings.Split(url, ",") {
		if a = strings.TrimSpace(a); a != "" {
			addrs = append(addrs, a)
		}
	}
	if len(addrs) == 0 {
		return nil, errors.New("kafka: no broker address parsed")
	}
	return addrs, nil
}

func (b *kafkaBroker) Name() string { return "kafka" }

func (b *kafkaBroker) writer(topic string) *kafkago.Writer {
	b.mu.Lock()
	defer b.mu.Unlock()
	w := b.writers[topic]
	if w == nil {
		w = &kafkago.Writer{
			Addr:         kafkago.TCP(b.addrs...),
			Topic:        topic,
			Balancer:     &kafkago.Hash{},
			RequiredAcks: -1, // 等全部副本
			BatchTimeout: 50 * time.Millisecond,
		}
		b.writers[topic] = w
	}
	return w
}

func (b *kafkaBroker) Publish(ctx context.Context, m Message) error {
	key := m.Key
	if key == "" {
		key = m.Event // 保证 Hash 分区器有稳定键
	}
	body, err := marshalMessage(m)
	if err != nil {
		return err
	}
	b.mu.Lock()
	closed := b.closed
	b.mu.Unlock()
	if closed {
		return errors.New("kafka: broker closed")
	}
	return b.writer(m.Topic).WriteMessages(ctx, kafkago.Message{
		Key:   []byte(key),
		Value: body,
		Headers: []kafkago.Header{
			{Key: "tenantId", Value: []byte(fmt.Sprintf("%d", m.TenantID))},
			{Key: "event", Value: []byte(m.Event)},
		},
	})
}

// Subscribe 非阻塞注册消费者（group 为消费组 ID，同组内负载均衡）。
func (b *kafkaBroker) Subscribe(ctx context.Context, topic, group string, h Handler) error {
	subCtx, cancel := context.WithCancel(ctx)
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		cancel()
		return errors.New("kafka: broker closed")
	}
	b.cancels = append(b.cancels, cancel)
	b.mu.Unlock()

	r := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers:        b.addrs,
		GroupID:        group,
		Topic:          topic,
		MinBytes:       1,
		MaxBytes:       10 << 20,
		CommitInterval: 0, // 仅显式提交
	})
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		defer r.Close()
		for {
			km, err := r.ReadMessage(subCtx)
			if subCtx.Err() != nil {
				return
			}
			if err != nil {
				log.Printf("[infra:kafka] read %s/%s: %v, retry in %s", group, topic, err, kafkaRedialGap)
				select {
				case <-time.After(kafkaRedialGap):
				case <-subCtx.Done():
					return
				}
				continue
			}
			m, err := unmarshalMessage(km.Value)
			if err != nil {
				log.Printf("[infra:kafka] bad message on %s: %v, drop", topic, err)
				_ = r.CommitMessages(subCtx, km)
				continue
			}
			var herr error
			for i := 1; i <= kafkaRetryMax; i++ {
				if herr = h(subCtx, m); herr == nil {
					break
				}
				time.Sleep(time.Duration(i) * 200 * time.Millisecond)
			}
			if herr != nil {
				log.Printf("[infra:kafka] handler %s failed after %d tries: %v, drop", m.Event, kafkaRetryMax, herr)
			}
			_ = r.CommitMessages(subCtx, km) // 成功或放弃后才提交
		}
	}()
	return nil
}

func (b *kafkaBroker) Close() error {
	b.mu.Lock()
	b.closed = true
	for _, cancel := range b.cancels {
		cancel()
	}
	ws := make([]*kafkago.Writer, 0, len(b.writers))
	for _, w := range b.writers {
		ws = append(ws, w)
	}
	b.mu.Unlock()
	b.wg.Wait()
	var firstErr error
	for _, w := range ws {
		if err := w.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
