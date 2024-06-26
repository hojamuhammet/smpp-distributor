package rabbitmq

import (
	"fmt"
	"smpp-distributor/internal/config"

	"github.com/streadway/amqp"
)

type RabbitMQ struct {
	conn    *amqp.Connection
	channel *amqp.Channel
}

func NewRabbitMQ(cfg config.RabbitMQ) (*RabbitMQ, error) {
	conn, err := amqp.Dial(cfg.URL)
	if err != nil {
		return nil, err
	}

	channel, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, err
	}

	// Declare the exchange
	err = channel.ExchangeDeclare(
		"messages", // name
		"direct",   // type
		true,       // durable
		false,      // auto-deleted
		false,      // internal
		false,      // no-wait
		nil,        // arguments
	)
	if err != nil {
		channel.Close()
		conn.Close()
		return nil, err
	}

	// Declare queues
	_, err = channel.QueueDeclare(
		"extra.turkmentv", // name
		true,              // durable
		false,             // delete when unused
		false,             // exclusive
		false,             // no-wait
		nil,               // arguments
	)
	if err != nil {
		channel.Close()
		conn.Close()
		return nil, err
	}

	_, err = channel.QueueDeclare(
		"sms.turkmentv", // name
		true,            // durable
		false,           // delete when unused
		false,           // exclusive
		false,           // no-wait
		nil,             // arguments
	)
	if err != nil {
		channel.Close()
		conn.Close()
		return nil, err
	}

	// Bind queues to the exchange with routing keys
	err = channel.QueueBind(
		"extra.turkmentv", // queue name
		"extra_key",       // routing key
		"messages",        // exchange
		false,
		nil,
	)
	if err != nil {
		channel.Close()
		conn.Close()
		return nil, err
	}

	err = channel.QueueBind(
		"sms.turkmentv", // queue name
		"sms_key",       // routing key
		"messages",      // exchange
		false,
		nil,
	)
	if err != nil {
		channel.Close()
		conn.Close()
		return nil, err
	}

	return &RabbitMQ{
		conn:    conn,
		channel: channel,
	}, nil
}

func (r *RabbitMQ) Close() {
	r.channel.Close()
	r.conn.Close()
}

func (r *RabbitMQ) Publish(queue string, src string, txt string) error {
	routingKey := ""
	switch queue {
	case "extra.turkmentv":
		routingKey = "extra_key"
	case "sms.turkmentv":
		routingKey = "sms_key"
	default:
		return fmt.Errorf("invalid queue: %s", queue)
	}

	body := fmt.Sprintf("src=%s, dst=%s, txt=%s", src, queue, txt)
	err := r.channel.Publish(
		"messages", // exchange
		routingKey, // routing key
		false,      // mandatory
		false,      // immediate
		amqp.Publishing{
			ContentType: "text/plain",
			Body:        []byte(body),
		})
	return err
}
