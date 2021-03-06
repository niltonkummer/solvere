package solvere

import (
	"errors"
	"fmt"
	// "strings"

	"github.com/miekg/dns"
)

var (
	ErrNSECMismatch         = errors.New("solvere: NSEC3 record doesn't match question")
	ErrNSECTypeExists       = errors.New("solvere: NSEC3 record shows question type exists")
	ErrNSECMultipleCoverage = errors.New("solvere: Multiple NSEC3 records cover next closer/source of synthesis")
	ErrNSECMissingCoverage  = errors.New("solvere: NSEC3 record missing for expected encloser")
	ErrNSECBadDelegation    = errors.New("solvere: DS or SOA bit set in NSEC3 type map")
	ErrNSECNSMissing        = errors.New("solvere: NS bit not set in NSEC3 type map")
	ErrNSECOptOut           = errors.New("solvere: Opt-Out bit not set for NSEC3 record covering next closer")
)

func typesSet(set []uint16, types ...uint16) bool {
	tm := make(map[uint16]struct{}, len(types))
	for _, t := range types {
		tm[t] = struct{}{}
	}
	for _, t := range set {
		if _, present := tm[t]; present {
			return true
		}
	}
	return false
}

// findClosestEncloser finds the Closest Encloser and Next Closers for a name
// in a set of NSEC3 records
func findClosestEncloser(name string, nsec []dns.RR) (string, string) {
	// RFC 5155 Section 8.3 (ish)
	labelIndices := dns.Split(name)
	nc := name
	for i := 0; i < len(labelIndices); i++ {
		z := name[labelIndices[i]:]
		_, err := findMatching(z, nsec)
		if err != nil {
			continue
		}
		if i != 0 {
			nc = name[labelIndices[i-1]:]
		}
		return z, nc
	}
	return "", ""
}

func findMatching(name string, nsec []dns.RR) ([]uint16, error) {
	for _, rr := range nsec {
		n := rr.(*dns.NSEC3)
		if n.Match(name) {
			return n.TypeBitMap, nil
		}
	}
	return nil, ErrNSECMissingCoverage
}

func findCoverer(name string, nsec []dns.RR) ([]uint16, bool, error) {
	for _, rr := range nsec {
		n := rr.(*dns.NSEC3)
		if n.Cover(name) {
			return n.TypeBitMap, (n.Flags & 1) == 1, nil
		}
	}
	return nil, false, ErrNSECMissingCoverage
}

// RFC 5155 Section 8.4
func verifyNameError(q *Question, nsec []dns.RR) error {
	ce, _ := findClosestEncloser(q.Name, nsec)
	if ce == "" {
		return ErrNSECMissingCoverage
	}
	_, _, err := findCoverer(fmt.Sprintf("*.%s", ce), nsec)
	if err != nil {
		return err
	}
	return nil
}

// verifyNODATA verifies NSEC/NSEC3 records from a answer with a NOERROR (0) RCODE
// and a empty Answer section
func verifyNODATA(q *Question, nsec []dns.RR) error {
	// RFC5155 Section 8.5
	types, err := findMatching(q.Name, nsec)
	if err != nil {
		if q.Type != dns.TypeDS {
			return err
		}

		// RFC5155 Section 8.6
		ce, nc := findClosestEncloser(q.Name, nsec)
		if ce == "" {
			return ErrNSECMissingCoverage
		}
		_, optOut, err := findCoverer(nc, nsec)
		if err != nil {
			return err
		}
		if !optOut {
			return ErrNSECOptOut
		}
		return nil
	}

	if typesSet(types, q.Type, dns.TypeCNAME) {
		return ErrNSECTypeExists
	}
	// BUG(roland): pretty sure this is 100% incorrect, should prob be its own method...
	// if strings.HasPrefix(q.Name, "*.") {
	// 	// RFC 5155 Section 8.7
	// 	ce, _ := findClosestEncloser(q.Name, nsec)
	// 	if ce == "" {
	// 		return ErrNSECMissingCoverage
	// 	}
	// 	matchTypes, err := findMatching(fmt.Sprintf("*.%s", ce), nsec)
	// 	if err != nil {
	// 		return err
	// 	}
	// 	if typesSet(matchTypes, q.Type, dns.TypeCNAME) {
	// 		return ErrNSECTypeExists
	// 	}
	// }
	return nil
}

// RFC 5155 Section 8.8
// func verifyWildcardAnswer() {
// }

// RFC 5155 Section 8.9
func verifyDelegation(delegation string, nsec []dns.RR) error {
	types, err := findMatching(delegation, nsec)
	if err != nil {
		ce, nc := findClosestEncloser(delegation, nsec)
		if ce == "" {
			return ErrNSECMissingCoverage
		}
		_, optOut, err := findCoverer(nc, nsec)
		if err != nil {
			return err
		}
		if !optOut {
			return ErrNSECOptOut
		}
		return nil
	}
	if !typesSet(types, dns.TypeNS) {
		return ErrNSECNSMissing
	}
	if typesSet(types, dns.TypeDS, dns.TypeSOA) {
		return ErrNSECBadDelegation
	}
	return nil
}
