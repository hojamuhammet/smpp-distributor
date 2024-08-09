package rabbitmq

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"smpp-distributor/internal/config"
	"smpp-distributor/pkg/logger"
	"smpp-distributor/pkg/utils"

	"github.com/streadway/amqp"
)

type SMSMessage struct {
	Source      string `json:"src"`
	Destination string `json:"dst"`
	Text        string `json:"txt"`
	Date        string `json:"date"`
	Parts       int    `json:"parts"`
}

type RabbitMQManager struct {
	conn        *amqp.Connection
	extraCh     *amqp.Channel
	smsCh       *amqp.Channel
	config      config.RabbitMQ
	logInstance *logger.Loggers
	mutex       sync.Mutex
	onClose     chan bool
}

func NewRabbitMQManager(cfg config.RabbitMQ, logInstance *logger.Loggers, onClose chan bool) (*RabbitMQManager, error) {
	r := &RabbitMQManager{
		config:      cfg,
		logInstance: logInstance,
		onClose:     onClose,
	}

	err := r.connect()
	if err != nil {
		return nil, err
	}

	go r.handleReconnect()
	return r, nil
}

func (r *RabbitMQManager) connect() error {
	var err error
	r.conn, err = amqp.Dial(r.config.URL)
	if err != nil {
		return err
	}

	r.extraCh, err = r.conn.Channel()
	if err != nil {
		r.conn.Close()
		return err
	}

	r.smsCh, err = r.conn.Channel()
	if err != nil {
		r.conn.Close()
		return err
	}

	err = r.declareResources()
	if err != nil {
		r.cleanup()
		return err
	}

	r.logInstance.InfoLogger.Info("Successfully connected to RabbitMQ with two channels.")
	return nil
}

func (r *RabbitMQManager) declareResources() error {
	// Declare resources for extra.turkmentv
	if err := r.extraCh.ExchangeDeclare(
		"extra_exchange", // example exchange name
		"direct",
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return err
	}

	if _, err := r.extraCh.QueueDeclare(
		"extra.turkmentv",
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return err
	}

	if err := r.extraCh.QueueBind(
		"extra.turkmentv",
		"extra.turkmentv",
		"extra_exchange",
		false,
		nil,
	); err != nil {
		return err
	}

	// Declare resources for sms.turkmentv
	if err := r.smsCh.ExchangeDeclare(
		"sms_exchange", // example exchange name
		"direct",
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return err
	}

	if _, err := r.smsCh.QueueDeclare(
		"sms.turkmentv",
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return err
	}

	if err := r.smsCh.QueueBind(
		"sms.turkmentv",
		"sms.turkmentv",
		"sms_exchange",
		false,
		nil,
	); err != nil {
		return err
	}

	return nil
}

func (r *RabbitMQManager) handleReconnect() {
	for {
		notifyClose := make(chan *amqp.Error)
		r.conn.NotifyClose(notifyClose)

		err := <-notifyClose
		r.logInstance.ErrorLogger.Error("RabbitMQ connection lost: ", utils.Err(err))
		r.onClose <- true // Notify the main application

		for {
			r.logInstance.InfoLogger.Info("Attempting to reconnect to RabbitMQ...")

			err := r.connect()
			if err == nil {
				r.logInstance.InfoLogger.Info("Successfully reconnected to RabbitMQ")
				r.onClose <- false // Notify the main application
				break
			}

			r.logInstance.ErrorLogger.Error(fmt.Sprintf("Failed to reconnect to RabbitMQ: %v", err))
			time.Sleep(5 * time.Second)
		}
	}
}

func (r *RabbitMQManager) Publish(queueName, src, dst, txt, date string, parts int) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	msg := SMSMessage{
		Source:      src,
		Destination: dst,
		Text:        txt,
		Date:        date,
		Parts:       parts,
	}

	body, err := json.Marshal(msg)
	if err != nil {
		r.logInstance.ErrorLogger.Error(fmt.Sprintf("Failed to marshal message to JSON: %v", err))
		return err
	}

	ch := r.getChannelForQueue(queueName)
	if ch == nil {
		return fmt.Errorf("failed to get channel for queue: %s", queueName)
	}

	for {
		err := ch.Publish(
			"",        // exchange
			queueName, // routing key
			false,     // mandatory
			false,     // immediate
			amqp.Publishing{
				ContentType:  "application/json",
				Body:         body,
				DeliveryMode: amqp.Persistent,
			},
		)

		if err == nil {
			return nil
		}

		r.logInstance.ErrorLogger.Error(fmt.Sprintf("Failed to publish message to RabbitMQ (queue %s): %v", queueName, err))

		if amqpErr, ok := err.(*amqp.Error); ok && (amqpErr.Code == amqp.ChannelError || amqpErr.Code == amqp.ConnectionForced) {
			r.logInstance.ErrorLogger.Error(fmt.Sprintf("Connection or Channel error occurred: %v", amqpErr))
			time.Sleep(5 * time.Second)
			continue
		}

		r.logInstance.ErrorLogger.Error(fmt.Sprintf("Unexpected error occurred while publishing: %v", err))
		return err
	}
}

func (r *RabbitMQManager) getChannelForQueue(queueName string) *amqp.Channel {
	switch queueName {
	case "extra.turkmentv":
		return r.extraCh
	case "sms.turkmentv":
		return r.smsCh
	default:
		r.logInstance.ErrorLogger.Error(fmt.Sprintf("No channel found for queue: %s", queueName))
		return nil
	}
}

func (r *RabbitMQManager) cleanup() {
	if r.extraCh != nil {
		r.extraCh.Close()
	}
	if r.smsCh != nil {
		r.smsCh.Close()
	}
	if r.conn != nil {
		r.conn.Close()
	}
	r.logInstance.InfoLogger.Info("RabbitMQ resources cleaned up")
}

func (r *RabbitMQManager) Close() {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.cleanup()
	r.logInstance.InfoLogger.Info("RabbitMQ connection and channels closed")
}
