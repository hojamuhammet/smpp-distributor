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

	return &RabbitMQ{
		conn:    conn,
		channel: channel,
	}, nil
}

func (r *RabbitMQ) Publish(queueName, src, dst, txt, date string, parts int) error {
	body := fmt.Sprintf("src=%s, dst=%s, txt=%s, date=%s, parts=%d", src, dst, txt, date, parts)

	err := r.channel.Publish(
		"",        // exchange (default)
		queueName, // queue name (routing key)
		false,     // mandatory
		false,     // immediate
		amqp.Publishing{
			ContentType: "text/plain",
			Body:        []byte(body),
		},
	)
	return err
}

func (r *RabbitMQ) Close() {
	r.channel.Close()
	r.conn.Close()
}
