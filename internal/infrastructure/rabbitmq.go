package rabbitmq

import (
	"fmt"
	"time"

	"smpp-distributor/internal/config"
	"smpp-distributor/pkg/logger"

	"github.com/streadway/amqp"
)

type RabbitMQ struct {
	conn        *amqp.Connection
	channel     *amqp.Channel
	config      config.RabbitMQ
	logInstance *logger.Loggers
}

func NewRabbitMQ(cfg config.RabbitMQ, logInstance *logger.Loggers) (*RabbitMQ, error) {
	r := &RabbitMQ{
		config:      cfg,
		logInstance: logInstance,
	}

	err := r.connect()
	if err != nil {
		return nil, err
	}

	return r, nil
}

func (r *RabbitMQ) connect() error {
	conn, err := amqp.Dial(r.config.URL)
	if err != nil {
		return err
	}

	channel, err := conn.Channel()
	if err != nil {
		conn.Close()
		return err
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
		return err
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
		return err
	}

	r.conn = conn
	r.channel = channel

	return nil
}

func (r *RabbitMQ) reconnect() error {
	r.logInstance.InfoLogger.Info("Attempting to reconnect to RabbitMQ...")
	var err error
	for i := 0; i < 5; i++ {
		err = r.connect()
		if err == nil {
			r.logInstance.InfoLogger.Info("Reconnected to RabbitMQ")
			return nil
		}
		r.logInstance.ErrorLogger.Error(fmt.Sprintf("Failed to reconnect to RabbitMQ (attempt %d): %v", i+1, err))
		time.Sleep(5 * time.Second)
	}
	return fmt.Errorf("failed to reconnect to RabbitMQ after multiple attempts: %w", err)
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

	if err != nil {
		if amqpErr, ok := err.(*amqp.Error); ok && amqpErr.Code == 504 {
			r.logInstance.ErrorLogger.Error(fmt.Sprintf("Connection closed with error %v, trying to reconnect...", amqpErr))
			reconnectErr := r.reconnect()
			if reconnectErr != nil {
				return reconnectErr
			}
			// Retry publishing after reconnection
			return r.Publish(queueName, src, dst, txt, date, parts)
		}
		return err
	}

	return nil
}

func (r *RabbitMQ) Close() {
	r.channel.Close()
	r.conn.Close()
}
