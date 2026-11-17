<?php
// The second WebDAV server for the webdav driver's integration tests:
// sabre/dav over a plain directory, behind HTTP Basic auth for one account.
//
// Stock classes only. The tree is sabre's own FSExt directory, locks are its
// file backend, and nothing here changes how a request is answered: what this
// server does with an If header, a MOVE into a missing parent or a conditional
// HEAD is sabre/dav's reading of the RFCs, not ours. See the Dockerfile for
// why there is a second server at all.
require __DIR__ . '/vendor/autoload.php';

use Sabre\DAV;

$server = new DAV\Server(new DAV\FSExt\Directory('/srv/dav'));
$server->setBaseUri('/');
$server->addPlugin(new DAV\Locks\Plugin(new DAV\Locks\Backend\File('/var/lib/sabredav/locks')));

$user = (string) getenv('WEBDAVD_USER');
$pass = (string) getenv('WEBDAVD_PASS');
$server->addPlugin(new DAV\Auth\Plugin(new DAV\Auth\Backend\BasicCallBack(
    function ($u, $p) use ($user, $pass) {
        return hash_equals($user, $u) && hash_equals($pass, $p);
    }
)));

$server->start();
