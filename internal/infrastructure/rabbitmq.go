package rabbitmq

import (
	"fmt"
	"sync"
	"time"

	"smpp-distributor/internal/config"
	"smpp-distributor/pkg/logger"
	"smpp-distributor/pkg/utils"

	"github.com/streadway/amqp"
)

type RabbitMQ struct {
	conn        *amqp.Connection
	channel     *amqp.Channel
	config      config.RabbitMQ
	logInstance *logger.Loggers
	mutex       sync.Mutex
}

// NewRabbitMQ creates a new RabbitMQ instance and connects to the RabbitMQ server
func NewRabbitMQ(cfg config.RabbitMQ, logInstance *logger.Loggers) (*RabbitMQ, error) {
	r := &RabbitMQ{
		config:      cfg,
		logInstance: logInstance,
	}

	err := r.connect()
	if err != nil {
		return nil, err
	}

	go r.handleReconnect()
	go r.monitorConnection()

	return r, nil
}

// connect establishes a connection and channel to RabbitMQ
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

	_, err = channel.QueueDeclare("extra.turkmentv", true, false, false, false, nil)
	if err != nil {
		channel.Close()
		conn.Close()
		return err
	}

	_, err = channel.QueueDeclare("sms.turkmentv", true, false, false, false, nil)
	if err != nil {
		channel.Close()
		conn.Close()
		return err
	}

	r.conn = conn
	r.channel = channel

	r.logInstance.InfoLogger.Info("Successfully connected to RabbitMQ")
	return nil
}

// handleReconnect handles reconnection attempts if the RabbitMQ connection is lost
func (r *RabbitMQ) handleReconnect() {
	for {
		notifyClose := make(chan *amqp.Error)
		r.conn.NotifyClose(notifyClose)

		err := <-notifyClose
		r.logInstance.ErrorLogger.Error("RabbitMQ connection lost: ", utils.Err(err))

		for {
			r.logInstance.InfoLogger.Info("Attempting to reconnect to RabbitMQ...")

			err := r.connect()
			if err == nil {
				r.logInstance.InfoLogger.Info("Successfully reconnected to RabbitMQ")
				break
			}

			r.logInstance.ErrorLogger.Error(fmt.Sprintf("Failed to reconnect to RabbitMQ: %v", err))
			time.Sleep(5 * time.Second)
		}
	}
}

// monitorConnection constantly checks the connection status and logs if the RabbitMQ service is down
func (r *RabbitMQ) monitorConnection() {
	for {
		time.Sleep(10 * time.Second)

		err := r.checkConnection()
		if err != nil {
			r.logInstance.ErrorLogger.Error(fmt.Sprintf("RabbitMQ connection check failed: %v", err))
		}
	}
}

// checkConnection performs a simple operation to verify the connection status
func (r *RabbitMQ) checkConnection() error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if r.channel == nil {
		return fmt.Errorf("RabbitMQ channel is nil")
	}

	// Perform a simple operation to check the connection
	err := r.channel.Publish(
		"",             // exchange
		"health_check", // routing key
		false,          // mandatory
		false,          // immediate
		amqp.Publishing{
			ContentType: "text/plain",
			Body:        []byte("health_check"),
		},
	)

	if err != nil {
		return fmt.Errorf("failed to publish health check message: %w", err)
	}

	return nil
}

// Publish sends a message to the specified queue, with retry logic on failure
func (r *RabbitMQ) Publish(queueName, src, dst, txt, date string, parts int) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	body := fmt.Sprintf("src=%s, dst=%s, txt=%s, date=%s, parts=%d", src, dst, txt, date, parts)

	for {
		err := r.channel.Publish(
			"",        // exchange
			queueName, // routing key
			false,     // mandatory
			false,     // immediate
			amqp.Publishing{
				ContentType: "text/plain",
				Body:        []byte(body),
			},
		)

		if err == nil {
			return nil
		}

		r.logInstance.ErrorLogger.Error(fmt.Sprintf("Failed to publish message to RabbitMQ: %v", err))

		if amqpErr, ok := err.(*amqp.Error); ok && (amqpErr.Code == amqp.ChannelError || amqpErr.Code == amqp.ConnectionForced) {
			r.logInstance.ErrorLogger.Error(fmt.Sprintf("Connection or Channel error occurred: %v", amqpErr))
			time.Sleep(5 * time.Second)
			continue
		}

		r.logInstance.ErrorLogger.Error(fmt.Sprintf("Unexpected error occurred while publishing: %v", err))
		return err
	}
}

// Close closes the RabbitMQ connection and channel
func (r *RabbitMQ) Close() {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if r.channel != nil {
		r.channel.Close()
	}
	if r.conn != nil {
		r.conn.Close()
	}
}
