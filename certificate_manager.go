package cruzbit

import (
	"crypto/tls"
	"log"
	"sync"
	"time"
)

// CertificateManager maintains one or more TLS certificates, and determines which to use for incoming Peer connections
type CertificateManager struct {
	lock     sync.RWMutex
	certSelf *tls.Certificate // generated, self-signed certificate
	certExt  *tls.Certificate // explicitly provided, external certificate
	extValid bool
	dataDir  string
	certPath string
	keyPath  string
}

// NewCertificateManager returns a new CertificateManager, creates the initial self-signed certificate,
//  and loads the external certificate, if file paths were provided in program config
func NewCertificateManager(dataDir, certPath, keyPath string) *CertificateManager {
	certificateManager := &CertificateManager{
		extValid: false,
		dataDir:  dataDir,
		certPath: certPath,
		keyPath:  keyPath,
	}

	// generate self-signed certificate
	certSelf, keySelf, err := generateSelfSignedCertAndKey(dataDir)
	if err != nil {
		log.Println("Unable to generate self-signed certificate and key files")
	} else {
		cert, err := tls.LoadX509KeyPair(certSelf, keySelf)
		if err != nil {
			log.Println("Unable to load self-signed certificate and key files")
		} else {
			certificateManager.certSelf = &cert
			log.Println("Generated self-signed TLS certificate and key")
		}
	}

	// load external certificate
	if len(certPath) != 0 && len(keyPath) != 0 {
		certExt, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			log.Println("Unable to load external TLS certificate")
		} else {
			// check newly loaded certificate
			certificateManager.certExt = &certExt
			notAfter, err := getTLSCertificateExpiry(certExt)
			if err != nil {
				log.Println("external certificate error: " + err.Error())
			} else {
				if time.Now().After(notAfter) {
					certificateManager.extValid = false
					log.Println("External TLS certificate is expired")
				} else {
					certificateManager.extValid = true
					log.Println("Loaded external TLS certificate and key")
				}
			}
		}
	}

	return certificateManager
}

// CheckCertificates is called periodically to ensure both the self-signed and external TLS certificates
//  are valid (not expired), and recreates or reloads them from disk where necessary
func (cm *CertificateManager) CheckCertificates() (time.Duration, error) {
	cm.lock.Lock()
	defer cm.lock.Unlock()

	now := time.Now()
	nextDuration := 3 * time.Minute
	var selfNotAfter, extNotAfter time.Time

	// check self cert; if invalid generate new
	selfNotAfter, err := getTLSCertificateExpiry(*cm.certSelf)
	if err != nil {
		log.Println("self-signed certificate error: " + err.Error())
	} else {
		if now.Add(certExpiryThreshold).After(selfNotAfter) {
			log.Println("The self-signed TLS certificate needs to be renewed")
			certSelf, keySelf, err := generateSelfSignedCertAndKey(cm.dataDir)
			if err != nil {
				return nextDuration, err
			}
			cert, err := tls.LoadX509KeyPair(certSelf, keySelf)
			if err != nil {
				return nextDuration, err
			}
			cm.certSelf = &cert
			log.Println("Generated new self-signed TLS certificate and key")
		}
	}

	// check ext cert, update extValid
	if cm.certExt != nil {
		extNotAfter, err := getTLSCertificateExpiry(*cm.certExt)
		if err != nil {
			log.Println("External certificate error: " + err.Error())
		} else {
			if now.Add(certExpiryThreshold).After(extNotAfter) {
				log.Println("The external TLS certificate needs to be renewed")
				certExt, err := tls.LoadX509KeyPair(cm.certPath, cm.keyPath)
				if err != nil {
					// couldn't load from disk, so check the cert in memory
					if now.After(extNotAfter) {
						cm.extValid = false
					} else {
						cm.extValid = true
					}
					return nextDuration, err
				} else {
					// check newly loaded certificate
					extNotAfter, err = getTLSCertificateExpiry(certExt)
					if err != nil {
						log.Println("Error loading certificate from disk: " + err.Error())
					} else {
						cm.certExt = &certExt
						if now.Add(certExpiryThreshold).After(extNotAfter) {
							if now.After(extNotAfter) {
								// cert on disk is expired
								cm.extValid = false
								log.Println("Reloaded external TLS certificate from disk, but it is expired")
							} else {
								// cert on disk expires soon (probably same as cert in memory)
								cm.extValid = true
							}
						} else {
							cm.extValid = true
							log.Println("Successfully reloaded external TLS certificate and key")
						}
					}
				}
			} else {
				cm.extValid = true
			}
		}
	}

	// determine when to next check on certificates
	if cm.extValid {
		// good ext cert; check again 24 hours before expiry
		nextDuration = extNotAfter.Add(-certExpiryThreshold).Sub(time.Now())
	} else {
		if len(cm.certPath) != 0 && len(cm.keyPath) != 0 {
			// expired ext cert, or no ext cert loaded but settings indicate there should be
			nextDuration = time.Hour
		} else {
			// no ext cert paths; check self cert 24 hours before expiry
			nextDuration = selfNotAfter.Add(-certExpiryThreshold).Sub(time.Now())
		}
	}
	// nextDuration sanity checks
	maxDuration := 24 * time.Hour * 28 // check at least once every 28 days
	minDuration := 3 * time.Minute     // must be positive int
	if maxDuration <= nextDuration {
		nextDuration = maxDuration
	}
	if nextDuration < minDuration {
		nextDuration = minDuration
	}
	return nextDuration, nil
}

// GetCertificateFunc is called from the PeerManager's httpd.Server when a new Listener is created,
//  ensuring that the most appropriate TLS certificate can be served when new connections are made
func (cm *CertificateManager) GetCertificateFunc() func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	return func(clientHello *tls.ClientHelloInfo) (*tls.Certificate, error) {
		cm.lock.RLock()
		defer cm.lock.RUnlock()
		if cm.extValid {
			return cm.certExt, nil
		} else {
			return cm.certSelf, nil
		}
	}
}
