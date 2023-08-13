import { useEffect } from 'react';
import { Navigate, useLocation, useSearchParams } from 'react-router-dom'
import { useApi } from '../context/apiContext';

export const Login = (): JSX.Element => {

    const { setJWT } = useApi()
    const [searchParams] = useSearchParams()
    const token = searchParams.get('token')
    const location = useLocation()

    useEffect(() => {
        if (!token) return
        setJWT(token)

    }, [token])

    return token ? (
        <Navigate to='/' state={{ from: location }} replace={true} />
    ) : (
        <>oops! something went wrong</>
    )
}
