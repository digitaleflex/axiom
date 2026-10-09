// Package agentpoll implémente la file d'attente par agent et la file des
// résultats, support de la boucle de dispatch Engine→Agent en mode NAT
// (ADR-0008 §17-28).
//
// L'exécuteur ne communique plus directement avec l'agent via agentclient :
// il pousse une operation (protocol.Operation encodée en JSON) dans la file de
// l'agent cible, et les routes POST /api/v1/agent/poll et
// POST /api/v1/agent/result lisent et écrivent sur ces files.
//
//   - La file d'attente des opérations par agent (queues) transporte les
//     operations brutes ; la route poll les consomme (consommation exclusive),
//     le résultat du consommateur est un échec : une operation never est
//     mise en double dans la file.
//
//   - La file de résultats (results) est indexée par (agentID, operationID) et
//     sert au replay : l'agent renvoie un acknowledgement + un result sur
//     POST /api/v1/agent/result, qui est stocké dans cette file.
//
// Les deux files sont synchronisées sur une map unique verrouillée : une
// opération est enlevée de la file d'attente dès qu'elle est remise au poll,
// et stockée dans la file de résultats lors de la réception de l'ack.
package agentpoll

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// ErrOperationNotFound signale une opération absente de la file.
var (
	ErrOperationNotFound = errors.New("agentpoll: operation not found in queue")
	ErrResultNotFound    = errors.New("agentpoll: result not yet stored")
)

// Operation est le transport d'une operation vers l'agent : l'ID, l'ID du
// déploiement associé et le corps encodé en JSON (protocol.Operation,
// ADR-0008 C4/C5).
type Operation struct {
	OperationID   string
	DeploymentID  string
	ApplicationID string
	Body          []byte
}

// Queue gère la file d'attente des operations par agent. Chaque agent dispose
// de son propre canal unificateur : une operation poussée arrive au poll de
// cet agent en FIFO, et le poll la consomme de manière exclusive.
//
// Timeout de lecture : 25s (ADR-0008 §21-22) — le poll bloque au maximum 25s
// en attendant qu'une operation soit disponible.
const ReadTimeout = 25 * time.Second

// Queue des operations par agent (un canal unifié par agentID).
type queue map[string]chan Operation

// Manager synchronise la file d'attente des operations et la file des
// résultats.
type Manager struct {
	sync.Mutex
	queues  queue                // agentID -> canal des operations en attente
	results map[string]Operation // (agentID, operationID) -> operation livrée
	log     *slog.Logger
}

// NewManager construit un nouveau Manager (file d'attente par agent). Un nil
// de Manager ne provoque pas de panique : les routes retournent
// un échec propre via errUnavailable.
func NewManager() *Manager {
	return &Manager{
		queues:  make(queue),
		results: make(map[string]Operation),
		log:     slog.Default(),
	}
}

func (m *Manager) logger() *slog.Logger {
	if m.log != nil {
		return m.log
	}
	return slog.Default()
}

// Enqueue place operation dans la file de agentID. Elle bloque jusqu'à ce que
// le poll de l'agent consume l'operation (capacité 1 par agent : un agent
// pollant ne peut être dépassé que par une operation en attente). Retourne
// ErrOperationNotFound si le poll de l'agent n'est pas prêt (queue morte ou
// agent non pollant).
func (m *Manager) Enqueue(ctx context.Context, agentID string, op Operation) error {
	m.Lock()
	c, ok := m.queues[agentID]
	if !ok {
		c = make(chan Operation, 1)
		m.queues[agentID] = c
	}
	m.Unlock()

	select {
	case c <- op:
		m.logger().Debug("operation enqueued for agent", "agentId", agentID, "operationId", op.OperationID)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Dequeue attend qu'une operation soit disponible dans la file de agentID,
// avec un timeout de 25s : l'appelant doit passer un context expirant à
// ReadTimeout, sinon la fonction bloque indéfiniment. L'operation est retirée
// de la file dès sa livraison (consommation exclusive) — le 200 du poll est
// l'acte de consommation. Retourne (operation, false) si le délai expire
// avant disponibilité.
func (m *Manager) Dequeue(ctx context.Context, agentID string) (Operation, bool, error) {
	if ctx == nil {
		return Operation{}, false, errors.New("agentpoll: nil context")
	}
	c, err := m.channelFor(agentID)
	if err != nil {
		return Operation{}, false, err
	}
	select {
	case op := <-c:
		return op, true, nil
	case <-ctx.Done():
		m.logger().Debug("poll timeout, no operation available", "agentId", agentID)
		return Operation{}, false, ctx.Err()
	}
}

// Ack enregistre l'acknowledgement du résultat dans la file de résultats
// (replay, #145). Une operation dont on reçoit l'ack est présumée avoir été
// remise au poll et donc enlevée de la file d'attente.
func (m *Manager) Ack(agentID, operationID string) {
	m.Lock()
	defer m.Unlock()
	key := resultKey(agentID, operationID)
	_, ok := m.results[key]
	if !ok {
		m.logger().Warn("acknowledgement for unknown operation", "agentId", agentID, "operationId", operationID)
		return
	}
	m.logger().Info("result acknowledged", "agentId", agentID, "operationId", operationID)
}

// Result enregistre le résultat complet d'une operation (agent→Engine via
// POST /api/v1/agent/result) dans la file de résultats, indexée par
// (agentID, operationID). Le replay peut ensuite le récupérer sans re-
// dispatch. Retourne ErrResultNotFound si aucune operation n'a encore été
// remise au poll pour cet agent/opération.
func (m *Manager) Result(agentID, operationID string, body []byte) error {
	m.Lock()
	defer m.Unlock()
	op, ok := m.results[resultKey(agentID, operationID)]
	if !ok {
		return ErrResultNotFound
	}
	op.Body = body
	m.results[resultKey(agentID, operationID)] = op
	return nil
}

// Get retrieves the result of a completed operation by application ID and
// operation ID for replay. If the result is not found, it returns a zero
// Operation and false.
func (m *Manager) GetResult(ctx context.Context, applicationID, operationID string) (Operation, error) {
	m.Lock()
	defer m.Unlock()
	op, ok := m.results[resultKey(applicationID, operationID)]
	if !ok {
		return Operation{}, ErrResultNotFound
	}
	_ = ctx // context reserved for future replay logic
	return op, nil
}

// ResultCount returns the number of stored results.
func (m *Manager) ResultCount() int {
	m.Lock()
	defer m.Unlock()
	return len(m.results)
}

// ResultClear removes all stored results.
func (m *Manager) ResultClear() {
	m.Lock()
	defer m.Unlock()
	m.results = make(map[string]Operation)
}

func (m *Manager) channelFor(agentID string) (chan Operation, error) {
	m.Lock()
	defer m.Unlock()
	c, ok := m.queues[agentID]
	if !ok {
		c = make(chan Operation, 1)
		m.queues[agentID] = c
	}
	return c, nil
}

func resultKey(agentID, operationID string) string {
	return agentID + "|" + operationID
}
